// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

const (
	serviceCatalogCorrelationMissingReason = "service catalog correlation evidence missing"
	serviceCatalogAnchorMissingReason      = "service/workload catalog anchor missing"
)

// SupplyChainImpactFindingResult is one reducer-owned vulnerability impact row
// returned by the public API.
//
// Priority fields are reducer-owned triage metadata. They explain urgency but
// never change impact_status, missing_evidence, or readiness truth.
type SupplyChainImpactFindingResult struct {
	FindingID             string                                  `json:"finding_id"`
	CVEID                 string                                  `json:"cve_id,omitempty"`
	AdvisoryID            string                                  `json:"advisory_id,omitempty"`
	PackageID             string                                  `json:"package_id,omitempty"`
	Ecosystem             string                                  `json:"ecosystem,omitempty"`
	PackageName           string                                  `json:"package_name,omitempty"`
	PURL                  string                                  `json:"purl,omitempty"`
	ProductCriteria       string                                  `json:"product_criteria,omitempty"`
	MatchCriteriaID       string                                  `json:"match_criteria_id,omitempty"`
	ObservedVersion       string                                  `json:"observed_version,omitempty"`
	RequestedRange        string                                  `json:"requested_range,omitempty"`
	FixedVersion          string                                  `json:"fixed_version,omitempty"`
	VulnerableRange       string                                  `json:"vulnerable_range,omitempty"`
	MatchReason           string                                  `json:"match_reason,omitempty"`
	ImpactStatus          string                                  `json:"impact_status"`
	Confidence            string                                  `json:"confidence,omitempty"`
	CVSSScore             float64                                 `json:"cvss_score,omitempty"`
	AdvisoryPublishedAt   string                                  `json:"advisory_published_at,omitempty"`
	AdvisoryUpdatedAt     string                                  `json:"advisory_updated_at,omitempty"`
	EPSSProbability       string                                  `json:"epss_probability,omitempty"`
	EPSSPercentile        string                                  `json:"epss_percentile,omitempty"`
	KnownExploited        bool                                    `json:"known_exploited"`
	PriorityReason        string                                  `json:"priority_reason,omitempty"`
	PriorityScore         int                                     `json:"priority_score,omitempty"`
	PriorityBucket        string                                  `json:"priority_bucket,omitempty"`
	PriorityReasonCodes   []string                                `json:"priority_reason_codes,omitempty"`
	PriorityContributions []SupplyChainImpactPriorityContribution `json:"priority_contributions,omitempty"`
	RuntimeReachability   string                                  `json:"runtime_reachability,omitempty"`
	Reachability          *SupplyChainReachabilityResult          `json:"reachability,omitempty"`
	RepositoryID          string                                  `json:"repository_id,omitempty"`
	SubjectDigest         string                                  `json:"subject_digest,omitempty"`
	ImageRef              string                                  `json:"image_ref,omitempty"`
	DependencyScope       string                                  `json:"dependency_scope,omitempty"`
	WorkloadIDs           []string                                `json:"workload_ids,omitempty"`
	DeploymentIDs         []string                                `json:"deployment_ids,omitempty"`
	ServiceIDs            []string                                `json:"service_ids,omitempty"`
	Environments          []string                                `json:"environments,omitempty"`
	CatalogEntityRefs     []string                                `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs      []string                                `json:"catalog_owner_refs,omitempty"`
	DependencyPath        []string                                `json:"dependency_path,omitempty"`
	DependencyDepth       int                                     `json:"dependency_depth,omitempty"`
	DirectDependency      *bool                                   `json:"direct_dependency,omitempty"`
	MissingEvidence       []string                                `json:"missing_evidence,omitempty"`
	EvidencePath          []string                                `json:"evidence_path,omitempty"`
	EvidenceFactIDs       []string                                `json:"evidence_fact_ids,omitempty"`
	SourceFreshness       string                                  `json:"source_freshness,omitempty"`
	SourceConfidence      string                                  `json:"source_confidence,omitempty"`
	Provenance            *SupplyChainImpactProvenance            `json:"provenance,omitempty"`
	// Suppression carries the reducer VEX/operator-policy decision attached
	// to this finding. The reducer always populates a decision (state=active
	// when nothing matched) so callers can audit suppression provenance even
	// when the finding is hidden from the default view.
	Suppression *SupplyChainSuppressionDecisionRow `json:"suppression,omitempty"`
	// DetectionProfile names whether the row meets the precise exact-version
	// bar or only the broader comprehensive owned-anchor profile.
	DetectionProfile string `json:"detection_profile,omitempty"`
	// Remediation is the reducer-owned advisory-only safe-upgrade
	// recommendation for this finding (issue #595). Older rows that predate
	// remediation computation omit this block.
	Remediation *SupplyChainImpactRemediation `json:"remediation,omitempty"`
	// DeploymentTruthTier classifies the strongest deployment evidence
	// available for this finding's repository or workload context, using the
	// shared truth.DeploymentTruthTier vocabulary (#5471). Omitted when the
	// finding has no deployment anchor at all.
	DeploymentTruthTier string `json:"deployment_truth_tier,omitempty"`
}

// SupplyChainImpactPriorityContribution explains one reducer priority input.
type SupplyChainImpactPriorityContribution struct {
	ReasonCode   string `json:"reason_code"`
	Input        string `json:"input"`
	Value        string `json:"value,omitempty"`
	Contribution int    `json:"contribution"`
}

// SupplyChainReachabilityResult is the stable reachability enrichment envelope
// attached to one vulnerability finding. It is separate from impact_status and
// confidence so callers cannot treat reachability absence as a clean result.
type SupplyChainReachabilityResult struct {
	State            string   `json:"state"`
	Confidence       string   `json:"confidence,omitempty"`
	Source           string   `json:"source,omitempty"`
	Evidence         string   `json:"evidence,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	LanguageMaturity string   `json:"language_maturity,omitempty"`
	MissingEvidence  []string `json:"missing_evidence,omitempty"`
}

func buildSupplyChainImpactFindingResult(row SupplyChainImpactFindingRow) SupplyChainImpactFindingResult {
	result := SupplyChainImpactFindingResult(row)
	result.MissingEvidence = normalizedSupplyChainImpactMissingEvidence(row)
	result.DeploymentTruthTier = string(supplyChainDeploymentTruthTier(row))
	return result
}

// supplyChainDeploymentTruthTier classifies the strongest deployment
// evidence tier available from the finding row's existing fields. It uses
// the shared truth.ClassifyDeploymentTruthTier with signals derived from
// what the reducer already writes in the finding payload.
//
// Live runtime evidence (runtime_confirmed) and CI provenance
// (provenance_ci_declared) are not currently differentiated in the
// reducer's finding payload — that enrichment is gated on #5472 (ci_cd
// correlation graph projection) and #5474 (gate extensions). Today all
// deployment-anchored findings classify as config_only.
func supplyChainDeploymentTruthTier(row SupplyChainImpactFindingRow) truth.DeploymentTruthTier {
	hasDeploymentAnchor := len(row.WorkloadIDs) > 0 ||
		len(row.DeploymentIDs) > 0 ||
		row.ImageRef != "" ||
		row.SubjectDigest != ""
	hasConfigEnvs := len(row.Environments) > 0
	return truth.ClassifyDeploymentTruthTier(
		false, // hasLiveEvidence: gated on #5472
		false, // instances not surfaced in finding row
		hasDeploymentAnchor,
		hasConfigEnvs,
	)
}

func normalizedSupplyChainImpactMissingEvidence(row SupplyChainImpactFindingRow) []string {
	missing := make([]string, 0, len(row.MissingEvidence))
	hasServiceCatalogEvidence := rowHasServiceCatalogEvidence(row)
	hasResolvedServiceCatalogAnchor := rowHasResolvedServiceCatalogAnchor(row)
	for _, reason := range row.MissingEvidence {
		if reason == serviceCatalogAnchorMissingReason && hasResolvedServiceCatalogAnchor {
			continue
		}
		if reason == serviceCatalogCorrelationMissingReason && hasServiceCatalogEvidence {
			if hasResolvedServiceCatalogAnchor {
				continue
			}
			reason = serviceCatalogAnchorMissingReason
		}
		missing = append(missing, reason)
	}
	return explanationUniqueStrings(missing)
}

func rowHasServiceCatalogEvidence(row SupplyChainImpactFindingRow) bool {
	for _, hop := range row.EvidencePath {
		if hop == serviceCatalogCorrelationFactKind {
			return true
		}
	}
	correlationFactIDPrefix := serviceCatalogCorrelationFactKind + ":"
	for _, factID := range row.EvidenceFactIDs {
		if strings.HasPrefix(factID, correlationFactIDPrefix) {
			return true
		}
	}
	return false
}

func rowHasResolvedServiceCatalogAnchor(row SupplyChainImpactFindingRow) bool {
	if len(row.ServiceIDs) > 0 {
		return true
	}
	return len(row.WorkloadIDs) > 0 && len(row.CatalogEntityRefs) > 0
}
