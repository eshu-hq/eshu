package query

import (
	"context"
	"strings"
	"time"
)

// SupplyChainImpactExplanationStore reads one reducer-owned vulnerability
// impact finding and the bounded evidence facts referenced by that finding.
type SupplyChainImpactExplanationStore interface {
	ExplainSupplyChainImpact(
		context.Context,
		SupplyChainImpactExplanationFilter,
	) (SupplyChainImpactExplanationRow, error)
}

// SupplyChainImpactExplanationFilter is the bounded input accepted by the
// vulnerability impact explanation route.
type SupplyChainImpactExplanationFilter struct {
	FindingID     string `json:"finding_id,omitempty"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	CVEID         string `json:"cve_id,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
}

// SupplyChainImpactExplanationRow contains the durable impact finding and only
// the source or reducer facts referenced by that finding's evidence ids.
type SupplyChainImpactExplanationRow struct {
	Finding       SupplyChainImpactFindingRow
	EvidenceFacts []SupplyChainImpactEvidenceFact
}

// SupplyChainImpactEvidenceFact is a bounded fact preview used to explain one
// reducer-owned vulnerability impact finding without returning raw graph paths
// or whole advisory bodies.
type SupplyChainImpactEvidenceFact struct {
	FactID           string
	FactKind         string
	SourceSystem     string
	SourceConfidence string
	ObservedAt       time.Time
	Payload          map[string]any
}

// SupplyChainImpactExplanationResult is the public API and MCP data payload for
// explaining one vulnerability finding or a bounded no-evidence scope.
type SupplyChainImpactExplanationResult struct {
	Outcome         string                                 `json:"outcome"`
	Input           SupplyChainImpactExplanationFilter     `json:"input"`
	Finding         *SupplyChainImpactFindingResult        `json:"finding,omitempty"`
	Advisory        SupplyChainImpactAdvisoryExplanation   `json:"advisory"`
	Component       SupplyChainImpactComponentExplanation  `json:"component"`
	Version         SupplyChainImpactVersionExplanation    `json:"version"`
	DependencyChain SupplyChainImpactDependencyChain       `json:"dependency_chain,omitempty"`
	Anchors         SupplyChainImpactExplanationAnchors    `json:"anchors"`
	Evidence        []SupplyChainImpactEvidenceFactSummary `json:"evidence"`
	Readiness       SupplyChainImpactReadinessEnvelope     `json:"readiness"`
	MissingEvidence []string                               `json:"missing_evidence,omitempty"`
	Freshness       SupplyChainImpactExplanationFreshness  `json:"freshness"`
}

// SupplyChainImpactAdvisoryExplanation summarizes advisory evidence selected
// for a finding and the source-reported vulnerable range when available.
type SupplyChainImpactAdvisoryExplanation struct {
	CVEID                      string                      `json:"cve_id,omitempty"`
	AdvisoryID                 string                      `json:"advisory_id,omitempty"`
	VulnerableRange            string                      `json:"vulnerable_range,omitempty"`
	RangeSource                string                      `json:"range_source,omitempty"`
	SelectedSeveritySource     string                      `json:"selected_severity_source,omitempty"`
	SelectedFixedVersionSource string                      `json:"selected_fixed_version_source,omitempty"`
	Sources                    []SupplyChainAdvisorySource `json:"sources,omitempty"`
	References                 []string                    `json:"references,omitempty"`
}

// SupplyChainImpactComponentExplanation identifies the package or component
// version matched by reducer-owned impact evidence.
type SupplyChainImpactComponentExplanation struct {
	PackageID       string `json:"package_id,omitempty"`
	Ecosystem       string `json:"ecosystem,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	PURL            string `json:"purl,omitempty"`
	ProductCriteria string `json:"product_criteria,omitempty"`
	MatchCriteriaID string `json:"match_criteria_id,omitempty"`
	ObservedVersion string `json:"observed_version,omitempty"`
	ManifestRange   string `json:"manifest_range,omitempty"`
}

// SupplyChainImpactVersionExplanation keeps version observations separate from
// advisory range and remediation metadata.
type SupplyChainImpactVersionExplanation struct {
	ObservedVersion string `json:"observed_version,omitempty"`
	ManifestRange   string `json:"manifest_range,omitempty"`
	VulnerableRange string `json:"vulnerable_range,omitempty"`
	FixedVersion    string `json:"fixed_version,omitempty"`
	VersionEvidence string `json:"version_evidence"`
}

// SupplyChainImpactDependencyChain explains direct versus transitive package
// evidence when a manifest, lockfile, or SBOM source provided it.
type SupplyChainImpactDependencyChain struct {
	Path             []string `json:"path,omitempty"`
	Depth            int      `json:"depth,omitempty"`
	DirectDependency *bool    `json:"direct_dependency,omitempty"`
}

// SupplyChainImpactExplanationAnchors lists scoped evidence anchors. Empty
// anchor families remain omitted rather than inferred from names or tags.
type SupplyChainImpactExplanationAnchors struct {
	RepositoryID    string                           `json:"repository_id,omitempty"`
	SubjectDigest   string                           `json:"subject_digest,omitempty"`
	ManifestPaths   []string                         `json:"manifest_paths,omitempty"`
	LockfilePaths   []string                         `json:"lockfile_paths,omitempty"`
	SBOMDocuments   []string                         `json:"sbom_documents,omitempty"`
	ImageDigests    []string                         `json:"image_digests,omitempty"`
	Workloads       []string                         `json:"workloads,omitempty"`
	ProviderAlerts  []SupplyChainProviderAlertAnchor `json:"provider_alerts,omitempty"`
	EvidenceFactIDs []string                         `json:"evidence_fact_ids,omitempty"`
}

// SupplyChainProviderAlertAnchor preserves provider alert evidence without
// promoting it into owned package, image, workload, or deployment truth.
type SupplyChainProviderAlertAnchor struct {
	Provider     string `json:"provider,omitempty"`
	AlertID      string `json:"alert_id,omitempty"`
	State        string `json:"state,omitempty"`
	ManifestPath string `json:"manifest_path,omitempty"`
}

// SupplyChainImpactEvidenceFactSummary is a compact source/reducer fact preview
// for one evidence id referenced by the finding.
type SupplyChainImpactEvidenceFactSummary struct {
	FactID           string `json:"fact_id"`
	FactKind         string `json:"fact_kind"`
	SourceSystem     string `json:"source_system,omitempty"`
	SourceConfidence string `json:"source_confidence,omitempty"`
	ObservedAt       string `json:"observed_at,omitempty"`
}

// SupplyChainImpactExplanationFreshness exposes evidence observation freshness
// for the explanation payload itself.
type SupplyChainImpactExplanationFreshness struct {
	State             string `json:"state"`
	LatestObservedAt  string `json:"latest_observed_at,omitempty"`
	EvidenceFactCount int    `json:"evidence_fact_count"`
}

// BuildSupplyChainImpactExplanation shapes a durable impact finding and its
// referenced evidence facts into one bounded explain response.
func BuildSupplyChainImpactExplanation(
	filter SupplyChainImpactExplanationFilter,
	row SupplyChainImpactExplanationRow,
	readiness SupplyChainImpactReadinessEnvelope,
) SupplyChainImpactExplanationResult {
	finding := SupplyChainImpactFindingResult(row.Finding)
	advisory := buildSupplyChainAdvisoryExplanation(row)
	component := buildSupplyChainComponentExplanation(row)
	version := buildSupplyChainVersionExplanation(row, advisory, component)
	anchors := buildSupplyChainExplanationAnchors(row)
	missing := explanationMissingEvidence(row.Finding, readiness, advisory, component, version, anchors)
	return SupplyChainImpactExplanationResult{
		Outcome:         "finding_explained",
		Input:           filter,
		Finding:         &finding,
		Advisory:        advisory,
		Component:       component,
		Version:         version,
		DependencyChain: buildSupplyChainDependencyChain(row.Finding, row.EvidenceFacts),
		Anchors:         anchors,
		Evidence:        summarizeSupplyChainEvidenceFacts(row.EvidenceFacts),
		Readiness:       readiness,
		MissingEvidence: missing,
		Freshness:       supplyChainExplanationFreshness(row.EvidenceFacts, readiness.Freshness),
	}
}

// BuildSupplyChainImpactNoEvidenceExplanation returns a bounded explanation for
// a valid scope where no reducer-owned impact finding currently exists.
func BuildSupplyChainImpactNoEvidenceExplanation(
	filter SupplyChainImpactExplanationFilter,
	readiness SupplyChainImpactReadinessEnvelope,
) SupplyChainImpactExplanationResult {
	missing := explanationUniqueStrings(append([]string{"impact_finding"}, readiness.MissingEvidence...))
	return SupplyChainImpactExplanationResult{
		Outcome:         "no_finding",
		Input:           filter,
		Advisory:        SupplyChainImpactAdvisoryExplanation{CVEID: filter.CVEID, AdvisoryID: filter.AdvisoryID},
		Component:       SupplyChainImpactComponentExplanation{PackageID: filter.PackageID},
		Version:         SupplyChainImpactVersionExplanation{VersionEvidence: "missing"},
		Anchors:         SupplyChainImpactExplanationAnchors{RepositoryID: filter.RepositoryID, SubjectDigest: filter.SubjectDigest},
		Evidence:        []SupplyChainImpactEvidenceFactSummary{},
		Readiness:       readiness,
		MissingEvidence: missing,
		Freshness:       SupplyChainImpactExplanationFreshness{State: explanationFreshnessState(readiness.Freshness)},
	}
}

func (f SupplyChainImpactExplanationFilter) hasBoundedScope() bool {
	if strings.TrimSpace(f.FindingID) != "" {
		return true
	}
	hasAdvisory := strings.TrimSpace(f.AdvisoryID) != "" || strings.TrimSpace(f.CVEID) != ""
	hasTarget := strings.TrimSpace(f.PackageID) != "" ||
		strings.TrimSpace(f.RepositoryID) != "" ||
		strings.TrimSpace(f.SubjectDigest) != ""
	return hasAdvisory && hasTarget
}

func (f SupplyChainImpactExplanationFilter) readinessScope() SupplyChainImpactTargetScope {
	return SupplyChainImpactTargetScope{
		CVEID:         f.CVEID,
		PackageID:     f.PackageID,
		RepositoryID:  f.RepositoryID,
		SubjectDigest: f.SubjectDigest,
	}
}

func findingReadinessScope(row SupplyChainImpactFindingRow, fallback SupplyChainImpactExplanationFilter) SupplyChainImpactTargetScope {
	scope := fallback.readinessScope()
	if scope.CVEID == "" {
		scope.CVEID = row.CVEID
	}
	if scope.PackageID == "" {
		scope.PackageID = row.PackageID
	}
	if scope.RepositoryID == "" {
		scope.RepositoryID = row.RepositoryID
	}
	if scope.SubjectDigest == "" {
		scope.SubjectDigest = row.SubjectDigest
	}
	return scope
}
