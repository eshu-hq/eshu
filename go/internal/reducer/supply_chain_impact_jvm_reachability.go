// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const jvmRuntimeReachabilityPackageAPIReachable = "jvm_package_api_reachable"

// JVMReachabilityFactFilter bounds active parser/SCIP evidence loading for
// Maven and Gradle impact findings.
type JVMReachabilityFactFilter struct {
	RepositoryIDs []string
	APIPackages   []string
}

type activeJVMReachabilityFactLoader interface {
	ListActiveJVMReachabilityFacts(context.Context, JVMReachabilityFactFilter) ([]facts.Envelope, error)
}

type jvmReachabilityIndex struct {
	usageByRepository map[string][]jvmReachabilityUsage
}

type jvmReachabilityUsage struct {
	apiPackage      string
	evidenceKind    string
	evidenceFactIDs []string
}

func (h SupplyChainImpactHandler) loadActiveJVMReachabilityFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeJVMReachabilityFactLoader)
	if !ok {
		return nil, nil
	}
	filter := jvmReachabilityFactFilter(envelopes)
	if len(filter.RepositoryIDs) == 0 || len(filter.APIPackages) == 0 {
		return nil, nil
	}
	reachabilityFacts, err := loader.ListActiveJVMReachabilityFacts(ctx, filter)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return reachabilityFacts, nil
}

func jvmReachabilityFactFilter(envelopes []facts.Envelope) JVMReachabilityFactFilter {
	var repositoryIDs []string
	var apiPackages []string
	for _, dependency := range extractPackageManifestDependencies(envelopes) {
		if !jvmPackageManager(dependency.PackageManager) {
			continue
		}
		if dependency.RepositoryID == "" || len(dependency.PackageAPIPackages) == 0 {
			continue
		}
		repositoryIDs = append(repositoryIDs, dependency.RepositoryID)
		apiPackages = append(apiPackages, dependency.PackageAPIPackages...)
	}
	return JVMReachabilityFactFilter{
		RepositoryIDs: uniqueSortedStrings(repositoryIDs),
		APIPackages:   uniqueSortedStrings(apiPackages),
	}
}

func buildJVMReachabilityIndex(envelopes []facts.Envelope) jvmReachabilityIndex {
	index := jvmReachabilityIndex{usageByRepository: map[string][]jvmReachabilityUsage{}}
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindFile || envelope.IsTombstone {
			continue
		}
		repositoryID := payloadStr(envelope.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		fileData, ok := envelope.Payload["parsed_file_data"].(map[string]any)
		if !ok || !jvmParsedFile(fileData, payloadStr(envelope.Payload, "relative_path")) {
			continue
		}
		index.usageByRepository[repositoryID] = append(
			index.usageByRepository[repositoryID],
			jvmReachabilityUsagesFromFile(envelope.FactID, fileData)...,
		)
	}
	return index
}

func jvmReachabilityUsagesFromFile(factID string, fileData map[string]any) []jvmReachabilityUsage {
	var usages []jvmReachabilityUsage
	for _, item := range mapSlice(fileData["imports"]) {
		if apiPackage := jvmAPIPackageFromValues(item["source"], item["name"], item["full_import_name"]); apiPackage != "" {
			usages = append(usages, jvmReachabilityUsage{
				apiPackage:      apiPackage,
				evidenceKind:    "parser_import",
				evidenceFactIDs: []string{factID},
			})
		}
	}
	for _, item := range mapSlice(fileData["function_calls"]) {
		if apiPackage := jvmAPIPackageFromValues(item["full_name"], item["inferred_obj_type"], item["name"]); apiPackage != "" {
			usages = append(usages, jvmReachabilityUsage{
				apiPackage:      apiPackage,
				evidenceKind:    "parser_call",
				evidenceFactIDs: []string{factID},
			})
		}
	}
	for _, item := range mapSlice(fileData["function_calls_scip"]) {
		if apiPackage := jvmAPIPackageFromValues(item["callee_symbol"], item["callee_name"]); apiPackage != "" {
			usages = append(usages, jvmReachabilityUsage{
				apiPackage:      apiPackage,
				evidenceKind:    "scip_call",
				evidenceFactIDs: []string{factID},
			})
		}
	}
	return usages
}

func applyJVMSupplyChainReachability(
	finding *SupplyChainImpactFinding,
	consumption supplyChainPackageConsumption,
	index supplyChainImpactIndex,
) []string {
	if !jvmPackageManager(finding.Ecosystem) {
		return nil
	}
	missing := jvmReachabilityBaselineMissingEvidence(consumption)
	if consumption.repositoryID == "" {
		return append(missing, "jvm repository evidence missing")
	}
	if !strings.EqualFold(consumption.dependencyResolutionState, "resolved") {
		return append(missing, "jvm dependency resolver evidence missing")
	}
	if len(consumption.packageAPIPackages) == 0 || strings.TrimSpace(consumption.packageAPIIdentitySource) == "" {
		return append(missing, "jvm package API identity evidence missing")
	}
	usage, ok := index.jvmReachability.match(consumption.repositoryID, consumption.packageAPIPackages)
	if !ok {
		return append(missing, "jvm parser or SCIP package usage evidence missing")
	}
	finding.RuntimeReachability = jvmRuntimeReachabilityPackageAPIReachable
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, usage.evidenceFactIDs...)
	finding.EvidencePath = append(finding.EvidencePath, factKindFile, usage.evidenceKind, consumption.packageAPIIdentitySource)
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = uniqueSortedStrings(finding.EvidencePath)
	return missing
}

func (index jvmReachabilityIndex) match(repositoryID string, apiPackages []string) (jvmReachabilityUsage, bool) {
	for _, usage := range index.usageByRepository[repositoryID] {
		for _, apiPackage := range apiPackages {
			if jvmPackageMatchesAPIPrefix(usage.apiPackage, apiPackage) ||
				jvmTextContainsAPIPrefix(usage.apiPackage, apiPackage) {
				return usage, true
			}
		}
	}
	return jvmReachabilityUsage{}, false
}

func jvmReachabilityBaselineMissingEvidence(consumption supplyChainPackageConsumption) []string {
	missing := []string{
		"jvm dependency-injection evidence incomplete",
		"jvm reflection evidence incomplete",
	}
	if strings.TrimSpace(consumption.sourceSet) == "" {
		missing = append(missing, "jvm source-set evidence missing")
	}
	if consumption.generatedCode == nil {
		missing = append(missing, "jvm generated-code evidence missing")
	}
	return missing
}

func jvmParsedFile(fileData map[string]any, relativePath string) bool {
	switch strings.ToLower(strings.TrimSpace(anyToString(fileData["lang"]))) {
	case "java", "kotlin", "scala":
		return true
	}
	switch {
	case strings.HasSuffix(relativePath, ".java"),
		strings.HasSuffix(relativePath, ".kt"),
		strings.HasSuffix(relativePath, ".kts"),
		strings.HasSuffix(relativePath, ".scala"),
		strings.HasSuffix(relativePath, ".sc"):
		return true
	default:
		return false
	}
}

func jvmAPIPackageFromValues(values ...any) string {
	for _, value := range values {
		if apiPackage := jvmAPIPackageFromText(anyToString(value)); apiPackage != "" {
			return apiPackage
		}
	}
	return ""
}

func jvmAPIPackageFromText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	normalized := strings.NewReplacer("/", ".", "#", ".", " ", ".").Replace(value)
	parts := strings.Split(normalized, ".")
	apiParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(apiParts) > 0 && jvmLooksLikeTypeOrMember(part) {
			break
		}
		if !jvmLooksLikePackageSegment(part) {
			continue
		}
		apiParts = append(apiParts, strings.ToLower(part))
	}
	if len(apiParts) < 2 {
		return ""
	}
	return strings.Join(apiParts, ".")
}

func jvmPackageMatchesAPIPrefix(observed string, provenPrefix string) bool {
	observed = strings.ToLower(strings.TrimSpace(observed))
	provenPrefix = strings.ToLower(strings.TrimSpace(provenPrefix))
	if observed == "" || provenPrefix == "" {
		return false
	}
	return observed == provenPrefix || strings.HasPrefix(observed, provenPrefix+".")
}

func jvmTextContainsAPIPrefix(observed string, provenPrefix string) bool {
	observed = strings.ToLower(strings.TrimSpace(observed))
	provenPrefix = strings.ToLower(strings.TrimSpace(provenPrefix))
	if observed == "" || provenPrefix == "" {
		return false
	}
	return strings.Contains("."+observed+".", "."+provenPrefix+".")
}

func jvmLooksLikePackageSegment(part string) bool {
	if part == "" {
		return false
	}
	for _, r := range part {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func jvmLooksLikeTypeOrMember(part string) bool {
	if part == "" {
		return false
	}
	first := rune(part[0])
	return first >= 'A' && first <= 'Z'
}

func jvmPackageManager(value string) bool {
	return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(value)) == packageidentity.EcosystemMaven
}
