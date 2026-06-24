// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"path"
	"sort"
	"strings"
	"time"
)

func buildSupplyChainAdvisoryExplanation(
	row SupplyChainImpactExplanationRow,
) SupplyChainImpactAdvisoryExplanation {
	finding := row.Finding
	out := SupplyChainImpactAdvisoryExplanation{
		CVEID:      finding.CVEID,
		AdvisoryID: finding.AdvisoryID,
	}
	if finding.Provenance != nil {
		out.RangeSource = finding.Provenance.SelectedRangeSource
		out.SelectedSeveritySource = finding.Provenance.SelectedSeveritySource
		out.SelectedFixedVersionSource = finding.Provenance.SelectedFixedVersionSource
		out.Sources = append([]SupplyChainAdvisorySource(nil), finding.Provenance.AdvisorySources...)
	}
	for _, fact := range row.EvidenceFacts {
		if !strings.Contains(fact.FactKind, "vulnerability") {
			continue
		}
		if out.VulnerableRange == "" {
			out.VulnerableRange = firstNonEmptyString(
				StringVal(fact.Payload, "affected_range"),
				StringVal(fact.Payload, "vulnerable_range"),
			)
		}
		if out.AdvisoryID == "" {
			out.AdvisoryID = StringVal(fact.Payload, "advisory_id")
		}
		out.References = append(out.References, StringSliceVal(fact.Payload, "references")...)
		out.References = append(out.References, StringSliceVal(fact.Payload, "reference_urls")...)
		if url := StringVal(fact.Payload, "url"); url != "" {
			out.References = append(out.References, url)
		}
	}
	out.References = explanationUniqueStrings(out.References)
	return out
}

func buildSupplyChainComponentExplanation(
	row SupplyChainImpactExplanationRow,
) SupplyChainImpactComponentExplanation {
	finding := row.Finding
	out := SupplyChainImpactComponentExplanation{
		PackageID:       finding.PackageID,
		Ecosystem:       finding.Ecosystem,
		PackageName:     finding.PackageName,
		PURL:            finding.PURL,
		ProductCriteria: finding.ProductCriteria,
		MatchCriteriaID: finding.MatchCriteriaID,
		ObservedVersion: finding.ObservedVersion,
	}
	for _, fact := range row.EvidenceFacts {
		if out.ManifestRange == "" {
			out.ManifestRange = firstNonEmptyString(
				StringVal(fact.Payload, "dependency_range"),
				StringVal(fact.Payload, "manifest_range"),
			)
		}
		if out.ObservedVersion == "" {
			out.ObservedVersion = StringVal(fact.Payload, "version")
		}
		if out.PURL == "" {
			out.PURL = StringVal(fact.Payload, "purl")
		}
	}
	return out
}

func buildSupplyChainVersionExplanation(
	row SupplyChainImpactExplanationRow,
	advisory SupplyChainImpactAdvisoryExplanation,
	component SupplyChainImpactComponentExplanation,
) SupplyChainImpactVersionExplanation {
	version := SupplyChainImpactVersionExplanation{
		ObservedVersion: component.ObservedVersion,
		ManifestRange:   component.ManifestRange,
		VulnerableRange: advisory.VulnerableRange,
		FixedVersion:    row.Finding.FixedVersion,
		VersionEvidence: "missing",
	}
	if version.FixedVersion == "" {
		for _, fact := range row.EvidenceFacts {
			if versions := StringSliceVal(fact.Payload, "fixed_versions"); len(versions) > 0 {
				version.FixedVersion = versions[0]
				break
			}
			if fixed := StringVal(fact.Payload, "fixed_version"); fixed != "" {
				version.FixedVersion = fixed
				break
			}
		}
	}
	switch {
	case version.ObservedVersion != "":
		version.VersionEvidence = "exact"
	case version.ManifestRange != "":
		version.VersionEvidence = "range_only"
	}
	return version
}

func buildSupplyChainDependencyChain(
	finding SupplyChainImpactFindingRow,
	facts []SupplyChainImpactEvidenceFact,
) *SupplyChainImpactDependencyChain {
	out := SupplyChainImpactDependencyChain{
		Path:             append([]string(nil), finding.DependencyPath...),
		Depth:            finding.DependencyDepth,
		DirectDependency: cloneBoolPointer(finding.DirectDependency),
	}
	for _, fact := range facts {
		if len(out.Path) == 0 {
			out.Path = StringSliceVal(fact.Payload, "dependency_path")
		}
		if out.Depth == 0 {
			out.Depth = IntVal(fact.Payload, "dependency_depth")
		}
		if out.DirectDependency == nil {
			out.DirectDependency = boolPointerVal(fact.Payload, "direct_dependency")
		}
	}
	if !dependencyChainHasEvidence(out) {
		return nil
	}
	return &out
}

func buildSupplyChainExplanationAnchors(
	row SupplyChainImpactExplanationRow,
) SupplyChainImpactExplanationAnchors {
	out := SupplyChainImpactExplanationAnchors{
		RepositoryID:    row.Finding.RepositoryID,
		SubjectDigest:   row.Finding.SubjectDigest,
		ImageRefs:       compactStrings([]string{row.Finding.ImageRef}),
		Workloads:       append([]string(nil), row.Finding.WorkloadIDs...),
		Deployments:     append([]string(nil), row.Finding.DeploymentIDs...),
		Services:        append([]string(nil), row.Finding.ServiceIDs...),
		Environments:    append([]string(nil), row.Finding.Environments...),
		CatalogEntities: append([]string(nil), row.Finding.CatalogEntityRefs...),
		CatalogOwners:   append([]string(nil), row.Finding.CatalogOwnerRefs...),
		EvidenceFactIDs: append([]string(nil), row.Finding.EvidenceFactIDs...),
	}
	for _, fact := range row.EvidenceFacts {
		out.EvidenceFactIDs = append(out.EvidenceFactIDs, fact.FactID)
		appendPathAnchors(&out, StringVal(fact.Payload, "relative_path"))
		appendPathAnchors(&out, StringVal(fact.Payload, "manifest_path"))
		appendLockfilePathAnchors(&out, StringVal(fact.Payload, "lockfile_path"))
		appendPathAnchors(&out, StringVal(fact.Payload, "source_path"))
		appendUniqueString(&out.SBOMDocuments, StringVal(fact.Payload, "document_id"))
		appendUniqueString(&out.ImageDigests, StringVal(fact.Payload, "digest"))
		appendUniqueString(&out.ImageDigests, StringVal(fact.Payload, "subject_digest"))
		appendUniqueString(&out.ImageRefs, StringVal(fact.Payload, "image_ref"))
		appendUniqueString(&out.Workloads, StringVal(fact.Payload, "workload_id"))
		appendDeploymentAnchors(&out, fact.Payload)
		appendUniqueString(&out.Services, StringVal(fact.Payload, "service_id"))
		appendUniqueString(&out.Environments, StringVal(fact.Payload, "environment"))
		appendUniqueString(&out.CatalogEntities, StringVal(fact.Payload, "entity_ref"))
		appendUniqueString(&out.CatalogOwners, StringVal(fact.Payload, "owner_ref"))
		if out.RepositoryID == "" {
			out.RepositoryID = StringVal(fact.Payload, "repository_id")
		}
		if out.SubjectDigest == "" {
			out.SubjectDigest = StringVal(fact.Payload, "subject_digest")
		}
		if alert := providerAlertAnchor(fact); alert.AlertID != "" || alert.Provider != "" {
			out.ProviderAlerts = append(out.ProviderAlerts, alert)
		}
	}
	out.EvidenceFactIDs = explanationUniqueStrings(out.EvidenceFactIDs)
	out.ManifestPaths = explanationUniqueStrings(out.ManifestPaths)
	out.LockfilePaths = explanationUniqueStrings(out.LockfilePaths)
	out.SBOMDocuments = explanationUniqueStrings(out.SBOMDocuments)
	out.ImageDigests = explanationUniqueStrings(out.ImageDigests)
	out.ImageRefs = explanationUniqueStrings(out.ImageRefs)
	out.Workloads = explanationUniqueStrings(out.Workloads)
	out.Deployments = explanationUniqueStrings(out.Deployments)
	out.Services = explanationUniqueStrings(out.Services)
	out.Environments = explanationUniqueStrings(out.Environments)
	return out
}

func appendDeploymentAnchors(out *SupplyChainImpactExplanationAnchors, payload map[string]any) {
	appendUniqueString(&out.Deployments, StringVal(payload, "deployment_id"))
	for _, entityKey := range StringSliceVal(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "deployment:") {
			appendUniqueString(&out.Deployments, entityKey)
		}
	}
}

func summarizeSupplyChainEvidenceFacts(
	facts []SupplyChainImpactEvidenceFact,
) []SupplyChainImpactEvidenceFactSummary {
	out := make([]SupplyChainImpactEvidenceFactSummary, 0, len(facts))
	for _, fact := range facts {
		summary := SupplyChainImpactEvidenceFactSummary{
			FactID:           fact.FactID,
			FactKind:         fact.FactKind,
			SourceSystem:     fact.SourceSystem,
			SourceConfidence: fact.SourceConfidence,
		}
		if !fact.ObservedAt.IsZero() {
			summary.ObservedAt = fact.ObservedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, summary)
	}
	return out
}

func explanationMissingEvidence(
	finding SupplyChainImpactFindingRow,
	readiness SupplyChainImpactReadinessEnvelope,
	advisory SupplyChainImpactAdvisoryExplanation,
	component SupplyChainImpactComponentExplanation,
	version SupplyChainImpactVersionExplanation,
	dependencyChain *SupplyChainImpactDependencyChain,
	anchors SupplyChainImpactExplanationAnchors,
) []string {
	missing := normalizedSupplyChainImpactMissingEvidence(finding)
	missing = append(missing, readiness.MissingEvidence...)
	if advisory.VulnerableRange == "" {
		missing = append(missing, "vulnerable_range")
	}
	if component.ObservedVersion == "" {
		missing = append(missing, "observed_version")
	}
	if version.FixedVersion == "" {
		missing = append(missing, "fixed_version")
	}
	if dependencyChain == nil && len(anchors.SBOMDocuments) == 0 {
		missing = append(missing, "dependency_chain")
	}
	return explanationUniqueStrings(missing)
}

func supplyChainExplanationFreshness(
	facts []SupplyChainImpactEvidenceFact,
	readinessFreshness string,
) SupplyChainImpactExplanationFreshness {
	var latest time.Time
	for _, fact := range facts {
		if fact.ObservedAt.After(latest) {
			latest = fact.ObservedAt
		}
	}
	out := SupplyChainImpactExplanationFreshness{
		State:             explanationFreshnessState(readinessFreshness),
		EvidenceFactCount: len(facts),
	}
	if !latest.IsZero() {
		out.LatestObservedAt = latest.UTC().Format(time.RFC3339)
	}
	return out
}

func explanationFreshnessState(readinessFreshness string) string {
	if strings.TrimSpace(readinessFreshness) != "" {
		return readinessFreshness
	}
	return FreshnessLabelUnknown
}

func providerAlertAnchor(fact SupplyChainImpactEvidenceFact) SupplyChainProviderAlertAnchor {
	if !strings.Contains(strings.ToLower(fact.FactKind), "alert") &&
		StringVal(fact.Payload, "alert_id") == "" {
		return SupplyChainProviderAlertAnchor{}
	}
	return SupplyChainProviderAlertAnchor{
		Provider:     StringVal(fact.Payload, "provider"),
		AlertID:      StringVal(fact.Payload, "alert_id"),
		State:        StringVal(fact.Payload, "state"),
		ManifestPath: firstNonEmptyString(StringVal(fact.Payload, "manifest_path"), StringVal(fact.Payload, "relative_path")),
	}
}

func appendPathAnchors(out *SupplyChainImpactExplanationAnchors, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	appendUniqueString(&out.ManifestPaths, path)
	if isKnownLockfilePath(path) {
		appendUniqueString(&out.LockfilePaths, path)
	}
}

func appendLockfilePathAnchors(out *SupplyChainImpactExplanationAnchors, lockfilePath string) {
	lockfilePath = strings.TrimSpace(lockfilePath)
	if lockfilePath == "" {
		return
	}
	appendUniqueString(&out.ManifestPaths, lockfilePath)
	appendUniqueString(&out.LockfilePaths, lockfilePath)
}

func dependencyChainHasEvidence(chain SupplyChainImpactDependencyChain) bool {
	return len(chain.Path) > 0 || chain.Depth > 0 || chain.DirectDependency != nil
}

func isKnownLockfilePath(candidate string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(candidate, "\\", "/")))
	switch base {
	case "bun.lock",
		"bun.lockb",
		"cargo.lock",
		"composer.lock",
		"gemfile.lock",
		"go.sum",
		"gradle.lockfile",
		"npm-shrinkwrap.json",
		"package-lock.json",
		"packages.lock.json",
		"paket.lock",
		"pdm.lock",
		"pipfile.lock",
		"pnpm-lock.yaml",
		"pnpm-lock.yml",
		"podfile.lock",
		"poetry.lock",
		"uv.lock",
		"yarn.lock":
		return true
	}
	return strings.HasSuffix(base, ".lock")
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func explanationUniqueStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
