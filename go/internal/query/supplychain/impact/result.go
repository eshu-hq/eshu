// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

const (
	ServiceCatalogCorrelationMissingReason = "service catalog correlation evidence missing"
	ServiceCatalogAnchorMissingReason      = "service/workload catalog anchor missing"
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
	// EnvironmentEvidence records, per environment name in Environments,
	// whether the strongest deployment evidence for that environment was
	// "deploy_event" or "declared" (issue #5426). Omitted when empty.
	EnvironmentEvidence map[string]string `json:"environment_evidence,omitempty"`
	// CIDeclaredArtifactDigest and CIDeclaredImageRef carry the matched
	// cicd_run_correlation deployment's OWN declared artifact identity
	// (issue #5469), persisted only when that deployment matched through a
	// strong artifact-identity branch (digest or image-ref equality), never
	// the weak repository+environment+operational-anchor branch. This is
	// the evidence version_resolution_corroboration's provenance_ci_declared
	// entry (when present) discloses as digest_or_version. Omitted when no
	// strong-branch CI/CD deployment evidence exists.
	CIDeclaredArtifactDigest string `json:"ci_declared_artifact_digest,omitempty"`
	CIDeclaredImageRef       string `json:"ci_declared_image_ref,omitempty"`
	// CloudRuntimeResourceRefs names the observed cloud compute resources
	// (running ECS task / image-package Lambda ARNs) whose running image digest
	// matches this finding's subject digest — runtime-observed deployment
	// evidence that drives the runtime_confirmed deployment_truth_tier, distinct
	// from the CI-declared deployment anchors (#5452).
	CloudRuntimeResourceRefs []string `json:"cloud_runtime_resource_refs,omitempty"`
	// KubernetesRuntimeWorkloadRefs carries exact digest-bound current workload
	// evidence. It is not the repository-level runtime_context workload list.
	KubernetesRuntimeWorkloadRefs []KubernetesRuntimeWorkloadRef `json:"kubernetes_runtime_workload_refs,omitempty"`
	// KubernetesRuntimeProbe reports the per-digest candidate cap and bounded
	// completeness state for the Kubernetes runtime graph probe.
	KubernetesRuntimeProbe *KubernetesRuntimeProbeMetadata `json:"kubernetes_runtime_probe,omitempty"`
	// RuntimeContext carries the read-time-resolved runtime context
	// (workloads, services, deployments, environments, catalog refs) resolved
	// from this finding's repository_id at query time (issue #5746). It is
	// labeled with truth_basis "read_time_resolved" so callers cannot mistake
	// it for baked workload_ids/service_ids/environments. The matching filters
	// resolve the same current repository mappings independently (#5747)
	// without mutating this reducer-owned payload.
	RuntimeContext    *SupplyChainRuntimeContextResult `json:"runtime_context,omitempty"`
	CatalogEntityRefs []string                         `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs  []string                         `json:"catalog_owner_refs,omitempty"`
	DependencyPath    []string                         `json:"dependency_path,omitempty"`
	DependencyDepth   int                              `json:"dependency_depth,omitempty"`
	DirectDependency  *bool                            `json:"direct_dependency,omitempty"`
	MissingEvidence   []string                         `json:"missing_evidence,omitempty"`
	EvidencePath      []string                         `json:"evidence_path,omitempty"`
	EvidenceFactIDs   []string                         `json:"evidence_fact_ids,omitempty"`
	SourceFreshness   string                           `json:"source_freshness,omitempty"`
	SourceConfidence  string                           `json:"source_confidence,omitempty"`
	Provenance        *SupplyChainImpactProvenance     `json:"provenance,omitempty"`
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
	// VersionResolutionTier discloses which deployment-truth tier the
	// judged version/digest for this finding was resolved from (#5469),
	// reusing the shared truth.DeploymentTruthTier vocabulary verbatim. It
	// can differ from DeploymentTruthTier: DeploymentTruthTier reports the
	// strongest tier with ANY deployment evidence, while
	// VersionResolutionTier requires that tier's evidence to also carry a
	// concrete version/digest claim. Omitted when the finding has no
	// version/digest evidence at all.
	VersionResolutionTier string `json:"version_resolution_tier,omitempty"`
	// VersionResolutionCorroboration lists every other deployment-truth tier
	// that also makes a version/digest claim for this finding -- including a
	// tier whose claim was ineligible to win (for example a CI-declared
	// digest that contradicts the finding's own subject_digest, review
	// finding R1) or whose claim disagrees with VersionResolutionTier's
	// judged value. Each entry's agreement field is a closed three-state
	// vocabulary (agrees/disagrees/not_comparable): a cross-axis comparison,
	// such as a config-materialized version against a digest-based winner,
	// is reported not_comparable rather than a misleading "disagrees"
	// (review finding R6). Omitted when no other tier makes a claim.
	VersionResolutionCorroboration []SupplyChainVersionResolutionCorroboration `json:"version_resolution_corroboration,omitempty"`
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

func BuildSupplyChainImpactFindingResult(row *SupplyChainImpactFindingRow) SupplyChainImpactFindingResult {
	result := SupplyChainImpactFindingResult(*row)
	result.MissingEvidence = normalizedSupplyChainImpactMissingEvidence(row)
	result.DeploymentTruthTier = string(supplyChainDeploymentTruthTier(row))
	result.VersionResolutionTier, result.VersionResolutionCorroboration = supplyChainVersionResolution(row)
	return result
}

// supplyChainDeploymentTruthTier classifies the strongest deployment
// evidence tier available from the finding row's existing fields, using the
// shared truth.DeploymentTruthTier vocabulary so this surface applies the
// same tiers as trace_deployment_chain and service story.
//
// The three deployment-evidence classes are now distinct (#5452):
//
//   - runtime_confirmed: a live cloud resource (running ECS task /
//     image-package Lambda) or current authorized Kubernetes workload actually
//     runs the finding's subject digest — surfaced at read time as
//     CloudRuntimeResourceRefs or KubernetesRuntimeWorkloadRefs.
//   - provenance_ci_declared: a cicd_run_correlation matched the finding (CI
//     declared the deployment). The match may be on the finding's digest or
//     image reference, or on repository plus environment plus an operational
//     anchor — the weaker branch still counts as CI-declared evidence here even
//     though it does not on its own raise runtime_reachability to
//     deployed_image (#5426). Before #5452 this collapsed into config_only
//     because no runtime tier existed on this surface.
//   - config_only: only config-materialized deployment anchors or config
//     environments exist, with no runtime or CI-declared evidence.
func supplyChainDeploymentTruthTier(row *SupplyChainImpactFindingRow) truth.DeploymentTruthTier {
	if len(row.CloudRuntimeResourceRefs) > 0 || len(row.KubernetesRuntimeWorkloadRefs) > 0 {
		return truth.ClassifyDeploymentTruthTier(true, false, false, false)
	}
	if rowHasCIDeclaredDeploymentEvidence(row) {
		return truth.TierProvenanceCIDeclared
	}
	hasDeploymentAnchor := len(row.WorkloadIDs) > 0 ||
		len(row.DeploymentIDs) > 0 ||
		row.ImageRef != "" ||
		row.SubjectDigest != ""
	hasConfigEnvs := len(row.Environments) > 0
	return truth.ClassifyDeploymentTruthTier(
		false, // hasLiveEvidence: no runtime-observed cloud resource
		false, // instances not surfaced in finding row
		hasDeploymentAnchor,
		hasConfigEnvs,
	)
}

// rowHasCIDeclaredDeploymentEvidence reports whether the finding row carries a
// cicd_run_correlation deployment hop — the reducer appends this evidence-path
// entry whenever a CI/CD run correlation matched the finding, so it is the
// row-level signal that a deployment was CI-declared.
//
// A match on the finding's digest or image reference is not required: a
// correlation that links only through repository plus environment plus an
// operational anchor also appends this hop, and deliberately so (#5426). Such a
// deployment does not on its own raise runtime_reachability to deployed_image,
// but it is still CI-declared deployment evidence and holds the row at
// deployment_truth_tier=provenance_ci_declared rather than config_only.
func rowHasCIDeclaredDeploymentEvidence(row *SupplyChainImpactFindingRow) bool {
	for _, hop := range row.EvidencePath {
		if hop == cicdRunCorrelationFactKind {
			return true
		}
	}
	return false
}

func normalizedSupplyChainImpactMissingEvidence(row *SupplyChainImpactFindingRow) []string {
	if supplyChainImpactMissingEvidenceIsNormalized(row) {
		return row.MissingEvidence
	}

	missing := make([]string, 0, len(row.MissingEvidence))
	hasServiceCatalogEvidence := rowHasServiceCatalogEvidence(row)
	hasResolvedServiceCatalogAnchor := rowHasResolvedServiceCatalogAnchor(row)
	for _, reason := range row.MissingEvidence {
		if reason == ServiceCatalogAnchorMissingReason && hasResolvedServiceCatalogAnchor {
			continue
		}
		if reason == ServiceCatalogCorrelationMissingReason && hasServiceCatalogEvidence {
			if hasResolvedServiceCatalogAnchor {
				continue
			}
			reason = ServiceCatalogAnchorMissingReason
		}
		missing = append(missing, reason)
	}
	return explanationUniqueStrings(missing)
}

// supplyChainImpactMissingEvidenceIsNormalized reports whether the existing
// slice is already the exact trimmed, unique, sorted output and no
// service-catalog reason needs rewriting. Reusing that slice avoids an
// otherwise unconditional per-finding allocation; rows and results already
// share the other immutable decoded slices.
func supplyChainImpactMissingEvidenceIsNormalized(row *SupplyChainImpactFindingRow) bool {
	hasServiceCatalogEvidence := rowHasServiceCatalogEvidence(row)
	hasResolvedServiceCatalogAnchor := rowHasResolvedServiceCatalogAnchor(row)
	for i, reason := range row.MissingEvidence {
		if reason == ServiceCatalogAnchorMissingReason && hasResolvedServiceCatalogAnchor {
			return false
		}
		if reason == ServiceCatalogCorrelationMissingReason && hasServiceCatalogEvidence {
			return false
		}
		if reason == "" || strings.TrimSpace(reason) != reason {
			return false
		}
		if i > 0 && row.MissingEvidence[i-1] >= reason {
			return false
		}
	}
	return true
}

func rowHasServiceCatalogEvidence(row *SupplyChainImpactFindingRow) bool {
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

func rowHasResolvedServiceCatalogAnchor(row *SupplyChainImpactFindingRow) bool {
	if len(row.ServiceIDs) > 0 {
		return true
	}
	return len(row.WorkloadIDs) > 0 && len(row.CatalogEntityRefs) > 0
}
