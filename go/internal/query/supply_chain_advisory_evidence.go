// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

const (
	advisoryEvidenceCapability       = "supply_chain.advisory_evidence.list"
	advisoryEvidenceMaxLimit         = 200
	advisoryEvidenceMaxFactRows      = 5000
	advisoryEvidenceFreshnessCurrent = "active"
)

// AdvisoryEvidenceStore reads source-only vulnerability advisory evidence.
type AdvisoryEvidenceStore interface {
	ListAdvisoryEvidence(context.Context, AdvisoryEvidenceFilter) ([]AdvisoryEvidenceRow, error)
}

// AdvisoryEvidenceFilter bounds source-evidence reads to an advisory, CVE,
// package, repository, service, or workload anchor. Repository, service, and
// workload anchors derive advisory lookups only from reducer-owned impact
// findings; provider-alert-only rows are not advisory evidence anchors.
type AdvisoryEvidenceFilter struct {
	CVEID            string
	AdvisoryID       string
	PackageID        string
	RepositoryID     string
	ServiceID        string
	WorkloadID       string
	Source           string
	AfterAdvisoryKey string
	Limit            int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). Advisory evidence facts are
	// global CVE/advisory data with no repository of their own, so the bare
	// cve_id/advisory_id/package_id path returns public advisory data
	// regardless of grants. The repository/service/workload-anchored path,
	// however, derives advisory anchors from reducer impact findings; when this
	// set is populated those impact findings are intersected with the granted
	// repositories so a scoped caller only learns which advisories affect its
	// own repositories. Empty means unrestricted (shared/admin/local).
	AllowedSourceRepositoryIDs []string
}

// AdvisoryEvidenceRow is one canonical advisory identity with source-specific
// evidence attached. It is source evidence only and does not imply repository,
// image, workload, or package impact.
type AdvisoryEvidenceRow struct {
	AdvisoryKey         string                       `json:"advisory_key"`
	CanonicalID         string                       `json:"canonical_id"`
	CVEIDs              []string                     `json:"cve_ids,omitempty"`
	GHSAIDs             []string                     `json:"ghsa_ids,omitempty"`
	OSVIDs              []string                     `json:"osv_ids,omitempty"`
	SourceIDs           []string                     `json:"source_ids,omitempty"`
	Sources             []AdvisorySourceEvidence     `json:"sources,omitempty"`
	AffectedPackages    []AdvisoryAffectedPackage    `json:"affected_packages,omitempty"`
	AffectedProducts    []AdvisoryAffectedProduct    `json:"affected_products,omitempty"`
	EPSS                []AdvisoryEPSSObservation    `json:"epss,omitempty"`
	KEV                 []AdvisoryKEVObservation     `json:"kev,omitempty"`
	References          []AdvisoryReferenceEvidence  `json:"references,omitempty"`
	SourceDisagreements []AdvisorySourceDisagreement `json:"source_disagreements,omitempty"`
	EvidenceFactIDs     []string                     `json:"evidence_fact_ids,omitempty"`
	LatestObservedAt    string                       `json:"latest_observed_at,omitempty"`
	SourceFreshness     string                       `json:"source_freshness,omitempty"`
	SourceConfidence    string                       `json:"source_confidence,omitempty"`
}

// AdvisorySourceEvidence preserves one source-reported advisory identity,
// severity, weakness, and withdrawal observation.
type AdvisorySourceEvidence struct {
	Source        string              `json:"source"`
	AdvisoryID    string              `json:"advisory_id,omitempty"`
	CVEID         string              `json:"cve_id,omitempty"`
	GHSAID        string              `json:"ghsa_id,omitempty"`
	Aliases       []string            `json:"aliases,omitempty"`
	PublishedAt   string              `json:"published_at,omitempty"`
	ModifiedAt    string              `json:"modified_at,omitempty"`
	WithdrawnAt   string              `json:"withdrawn_at,omitempty"`
	SeverityLabel string              `json:"severity_label,omitempty"`
	CVSSScore     float64             `json:"cvss_score,omitempty"`
	CVSSVector    string              `json:"cvss_vector,omitempty"`
	CVSSVectorV2  string              `json:"cvss_v2,omitempty"`
	CVSSVectorV3  string              `json:"cvss_v3,omitempty"`
	CVSSVectorV4  string              `json:"cvss_v4,omitempty"`
	CVSSMetrics   map[string]any      `json:"cvss_metrics,omitempty"`
	Severity      []map[string]string `json:"severity,omitempty"`
	CWEs          []string            `json:"cwes,omitempty"`
	SourceFactIDs []string            `json:"source_fact_ids,omitempty"`
}

// AdvisoryAffectedPackage preserves package-native affected range and fixed
// version evidence from OSV, GHSA, GLAD, or vendor package advisories.
type AdvisoryAffectedPackage struct {
	Source              string           `json:"source"`
	AdvisoryID          string           `json:"advisory_id,omitempty"`
	CVEID               string           `json:"cve_id,omitempty"`
	GHSAID              string           `json:"ghsa_id,omitempty"`
	Ecosystem           string           `json:"ecosystem,omitempty"`
	PackageID           string           `json:"package_id,omitempty"`
	PURL                string           `json:"purl,omitempty"`
	AffectedRange       string           `json:"affected_range,omitempty"`
	ParsedAffectedRange map[string]any   `json:"parsed_affected_range,omitempty"`
	AffectedRanges      []map[string]any `json:"affected_ranges,omitempty"`
	AffectedVersions    []string         `json:"affected_versions,omitempty"`
	FixedVersions       []string         `json:"fixed_versions,omitempty"`
	SourceFactID        string           `json:"source_fact_id,omitempty"`
}

// AdvisoryAffectedProduct preserves NVD product/CPE applicability evidence.
type AdvisoryAffectedProduct struct {
	Source                      string `json:"source"`
	CVEID                       string `json:"cve_id,omitempty"`
	Criteria                    string `json:"criteria,omitempty"`
	MatchCriteriaID             string `json:"match_criteria_id,omitempty"`
	Vulnerable                  bool   `json:"vulnerable"`
	VersionStartIncluding       string `json:"version_start_including,omitempty"`
	VersionStartExcluding       string `json:"version_start_excluding,omitempty"`
	VersionEndIncluding         string `json:"version_end_including,omitempty"`
	VersionEndExcluding         string `json:"version_end_excluding,omitempty"`
	SourceConfigurationOperator string `json:"source_configuration_operator,omitempty"`
	SourceConfigurationNegate   bool   `json:"source_configuration_negate,omitempty"`
	SourceNodeOperator          string `json:"source_node_operator,omitempty"`
	SourceNodeNegate            bool   `json:"source_node_negate,omitempty"`
	SourceFactID                string `json:"source_fact_id,omitempty"`
}

// AdvisoryEPSSObservation preserves one FIRST EPSS score observation.
type AdvisoryEPSSObservation struct {
	Source      string `json:"source"`
	CVEID       string `json:"cve_id,omitempty"`
	Probability string `json:"probability,omitempty"`
	Percentile  string `json:"percentile,omitempty"`
	ScoreDate   string `json:"score_date,omitempty"`
	FactID      string `json:"fact_id,omitempty"`
}

// AdvisoryKEVObservation preserves one CISA KEV known-exploited observation.
type AdvisoryKEVObservation struct {
	Source                     string   `json:"source"`
	CVEID                      string   `json:"cve_id,omitempty"`
	DateAdded                  string   `json:"date_added,omitempty"`
	RequiredAction             string   `json:"required_action,omitempty"`
	DueDate                    string   `json:"due_date,omitempty"`
	KnownRansomwareCampaignUse string   `json:"known_ransomware_campaign_use,omitempty"`
	CWEs                       []string `json:"cwes,omitempty"`
	FactID                     string   `json:"fact_id,omitempty"`
}

// AdvisoryReferenceEvidence preserves one sanitized source reference URL.
type AdvisoryReferenceEvidence struct {
	Source        string `json:"source"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	CVEID         string `json:"cve_id,omitempty"`
	ReferenceType string `json:"reference_type,omitempty"`
	URL           string `json:"url,omitempty"`
	FactID        string `json:"fact_id,omitempty"`
}

// AdvisorySourceDisagreement records a source-level disagreement without
// selecting a winner.
type AdvisorySourceDisagreement struct {
	Field  string                      `json:"field"`
	Values []AdvisoryDisagreementValue `json:"values"`
}

// AdvisoryDisagreementValue is one source/value pair inside a disagreement.
type AdvisoryDisagreementValue struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}
