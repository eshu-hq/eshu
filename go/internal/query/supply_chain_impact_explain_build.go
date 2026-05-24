package query

import (
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
) SupplyChainImpactDependencyChain {
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
	return out
}

func buildSupplyChainExplanationAnchors(
	row SupplyChainImpactExplanationRow,
) SupplyChainImpactExplanationAnchors {
	out := SupplyChainImpactExplanationAnchors{
		RepositoryID:    row.Finding.RepositoryID,
		SubjectDigest:   row.Finding.SubjectDigest,
		EvidenceFactIDs: append([]string(nil), row.Finding.EvidenceFactIDs...),
	}
	for _, fact := range row.EvidenceFacts {
		out.EvidenceFactIDs = append(out.EvidenceFactIDs, fact.FactID)
		appendPathAnchors(&out, StringVal(fact.Payload, "relative_path"))
		appendPathAnchors(&out, StringVal(fact.Payload, "manifest_path"))
		appendPathAnchors(&out, StringVal(fact.Payload, "lockfile_path"))
		appendPathAnchors(&out, StringVal(fact.Payload, "source_path"))
		appendUniqueString(&out.SBOMDocuments, StringVal(fact.Payload, "document_id"))
		appendUniqueString(&out.ImageDigests, StringVal(fact.Payload, "digest"))
		appendUniqueString(&out.ImageDigests, StringVal(fact.Payload, "subject_digest"))
		appendUniqueString(&out.Workloads, StringVal(fact.Payload, "workload_id"))
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
	out.Workloads = explanationUniqueStrings(out.Workloads)
	return out
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
	anchors SupplyChainImpactExplanationAnchors,
) []string {
	missing := append([]string(nil), finding.MissingEvidence...)
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
	if len(finding.DependencyPath) == 0 && len(anchors.SBOMDocuments) == 0 {
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
	lower := strings.ToLower(path)
	if strings.Contains(lower, "lock") ||
		strings.HasSuffix(lower, "go.sum") ||
		strings.HasSuffix(lower, "pnpm-lock.yaml") ||
		strings.HasSuffix(lower, "yarn.lock") {
		appendUniqueString(&out.LockfilePaths, path)
	}
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
