// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	jsTSPackageAPICallEvidence       = "package_api_call"
	jsTSPackageAPIImportEvidence     = "package_api_import"
	jsTSPackageAPIReExportEvidence   = "package_api_reexport"
	jsTSPackageAPISCIPCallEvidence   = "package_api_scip_call"
	jsTSPackageAPIUnknownEvidence    = "package_api_unknown"
	jsTSPackageAPIAmbiguousEvidence  = "package_api_ambiguous"
	jsTSPackageAPIMissingEvidence    = "package_api_missing_evidence"
	jsTSPackageAPIMissingReason      = "javascript/typescript package API evidence missing"
	jsTSPackageAPIAmbiguousReason    = "javascript/typescript package API identity ambiguous"
	jsTSParserOrSCIPMissingReason    = "javascript/typescript parser or SCIP package API evidence missing"
	jsTSPackageIdentityMissingReason = "javascript/typescript npm package identity missing"
)

type jsTSPackageReachabilityIndex struct {
	repositories map[string]*jsTSRepositoryReachability
}

type jsTSRepositoryReachability struct {
	packages        map[string]jsTSPackageAPIUsage
	ambiguousBases  map[string]struct{}
	hasParserOrSCIP bool
}

type jsTSPackageAPIUsage struct {
	evidence string
	source   string
	factID   string
}

func buildJSTSPackageReachabilityIndex(envelopes []facts.Envelope) jsTSPackageReachabilityIndex {
	index := jsTSPackageReachabilityIndex{repositories: map[string]*jsTSRepositoryReachability{}}
	for _, envelope := range envelopes {
		if envelope.IsTombstone || envelope.FactKind != factKindFile {
			continue
		}
		repositoryID := firstNonBlank(
			payloadStr(envelope.Payload, "repo_id"),
			payloadStr(envelope.Payload, "repository_id"),
			envelope.ScopeID,
		)
		if repositoryID == "" {
			continue
		}
		fileData, ok := envelope.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		if !isJSTSParsedFile(fileData) {
			continue
		}
		repo := index.repository(repositoryID)
		repo.hasParserOrSCIP = true
		repo.recordImports(fileData, envelope.FactID)
		repo.recordSCIPEdges(fileData, envelope.FactID)
	}
	return index
}

func (i jsTSPackageReachabilityIndex) repository(repositoryID string) *jsTSRepositoryReachability {
	repositoryID = strings.TrimSpace(repositoryID)
	repo := i.repositories[repositoryID]
	if repo != nil {
		return repo
	}
	repo = &jsTSRepositoryReachability{
		packages:       map[string]jsTSPackageAPIUsage{},
		ambiguousBases: map[string]struct{}{},
	}
	i.repositories[repositoryID] = repo
	return repo
}

func (r *jsTSRepositoryReachability) recordImports(fileData map[string]any, factID string) {
	importAliases := map[string]string{}
	for _, entry := range mapSlice(fileData["imports"]) {
		source := payloadStr(entry, "source")
		packageRoot, ok := npmPackageRootFromImportSource(source)
		if !ok {
			continue
		}
		evidence := jsTSPackageAPIImportEvidence
		if strings.EqualFold(payloadStr(entry, "import_type"), "reexport") {
			evidence = jsTSPackageAPIReExportEvidence
		}
		r.recordUsage(packageRoot, jsTSPackageAPIUsage{
			evidence: evidence,
			source:   "parser_js_ts",
			factID:   factID,
		})
		for _, alias := range jsTSImportAliases(entry) {
			importAliases[alias] = packageRoot
		}
	}
	for _, call := range mapSlice(fileData["function_calls"]) {
		callName := firstNonBlank(payloadStr(call, "full_name"), payloadStr(call, "name"))
		for alias, packageRoot := range importAliases {
			if jsTSCallReferencesImportAlias(callName, alias) {
				r.recordUsage(packageRoot, jsTSPackageAPIUsage{
					evidence: jsTSPackageAPICallEvidence,
					source:   "parser_js_ts",
					factID:   factID,
				})
			}
		}
	}
}

func (r *jsTSRepositoryReachability) recordSCIPEdges(fileData map[string]any, factID string) {
	for _, edge := range mapSlice(fileData["function_calls_scip"]) {
		for _, key := range []string{"package", "package_name", "import_source", "callee_package", "callee_symbol"} {
			packageRoot, ok := npmPackageRootFromSCIPValue(payloadStr(edge, key))
			if !ok {
				continue
			}
			r.recordUsage(packageRoot, jsTSPackageAPIUsage{
				evidence: jsTSPackageAPISCIPCallEvidence,
				source:   "scip_js_ts",
				factID:   factID,
			})
		}
	}
}

func (r *jsTSRepositoryReachability) recordUsage(packageRoot string, usage jsTSPackageAPIUsage) {
	key := normalizeNPMName(packageRoot)
	if key == "" {
		return
	}
	if base := npmPackageBaseName(key); base != "" && base != key {
		r.ambiguousBases[base] = struct{}{}
	}
	current, ok := r.packages[key]
	if !ok || jsTSPackageAPIEvidencePriority(usage.evidence) < jsTSPackageAPIEvidencePriority(current.evidence) {
		r.packages[key] = usage
	}
}

func applyJSTSPackageReachability(
	finding *SupplyChainImpactFinding,
	index supplyChainImpactIndex,
) []string {
	if finding == nil || normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "npm" {
		return nil
	}
	if finding.Status == SupplyChainImpactNotAffectedKnownFixed {
		return nil
	}
	packageName := npmPackageNameForReachability(*finding)
	if packageName == "" {
		finding.RuntimeReachability = jsTSPackageAPIMissingEvidence
		return []string{jsTSPackageIdentityMissingReason}
	}
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	repo := index.jsTSPackageReachability.repositories[repositoryID]
	if repo == nil || !repo.hasParserOrSCIP {
		finding.RuntimeReachability = jsTSPackageAPIMissingEvidence
		return []string{jsTSParserOrSCIPMissingReason}
	}
	normalizedPackage := normalizeNPMName(packageName)
	if usage, ok := repo.packages[normalizedPackage]; ok {
		finding.RuntimeReachability = usage.evidence
		if usage.factID != "" {
			finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, usage.factID)
			finding.EvidencePath = append(finding.EvidencePath, factKindFile)
		}
		return nil
	}
	baseName := npmPackageBaseName(normalizedPackage)
	_, scopedBaseWasSeen := repo.ambiguousBases[baseName]
	_, unscopedBaseWasSeen := repo.packages[baseName]
	if scopedBaseWasSeen || (strings.HasPrefix(normalizedPackage, "@") && unscopedBaseWasSeen) {
		finding.RuntimeReachability = jsTSPackageAPIAmbiguousEvidence
		return []string{jsTSPackageAPIAmbiguousReason}
	}
	finding.RuntimeReachability = jsTSPackageAPIUnknownEvidence
	return []string{jsTSPackageAPIMissingReason}
}

func isJSTSParsedFile(fileData map[string]any) bool {
	switch strings.ToLower(firstNonBlank(
		payloadStr(fileData, "language"),
		payloadStr(fileData, "lang"),
	)) {
	case "javascript", "jsx", "typescript", "tsx":
		return true
	default:
		return false
	}
}

func jsTSImportAliases(entry map[string]any) []string {
	return uniqueSortedStrings([]string{
		payloadStr(entry, "alias"),
		payloadStr(entry, "name"),
	})
}

func jsTSCallReferencesImportAlias(callName string, alias string) bool {
	callName = strings.TrimSpace(callName)
	alias = strings.TrimSpace(alias)
	if callName == "" || alias == "" || alias == "default" || alias == "*" {
		return false
	}
	return callName == alias || strings.HasPrefix(callName, alias+".")
}

func npmPackageRootFromImportSource(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
		return "", false
	}
	if strings.HasPrefix(source, "node:") {
		return "", false
	}
	parts := strings.Split(source, "/")
	if strings.HasPrefix(source, "@") {
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", false
		}
		return parts[0] + "/" + parts[1], true
	}
	if parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

func npmPackageRootFromSCIPValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	fields := strings.Fields(value)
	for i, field := range fields {
		if strings.EqualFold(field, "npm") && i+1 < len(fields) {
			return npmPackageRootFromImportSource(fields[i+1])
		}
	}
	return npmPackageRootFromImportSource(value)
}

func npmPackageNameForReachability(finding SupplyChainImpactFinding) string {
	for _, candidate := range []string{
		finding.PackageName,
		packageNameFromPURL(finding.PURL),
		packageNameFromPURL(finding.PackageID),
		packageNameFromPackageID(finding.PackageID),
	} {
		if normalized := normalizeNPMName(candidate); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeNPMName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func npmPackageBaseName(packageName string) string {
	packageName = normalizeNPMName(packageName)
	if strings.HasPrefix(packageName, "@") {
		parts := strings.Split(packageName, "/")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	if before, _, ok := strings.Cut(packageName, "/"); ok {
		return before
	}
	return packageName
}

func jsTSPackageAPIEvidencePriority(evidence string) int {
	switch evidence {
	case jsTSPackageAPICallEvidence, jsTSPackageAPISCIPCallEvidence:
		return 0
	case jsTSPackageAPIReExportEvidence:
		return 1
	case jsTSPackageAPIImportEvidence:
		return 2
	default:
		return 3
	}
}
