// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// SupplyChainImpactFinding is the schema-version-1 payload for
// "reducer_supply_chain_impact_finding". It captures the reducer's durable
// vulnerability impact decision and the evidence anchors API/MCP read surfaces
// use to explain that decision.
type SupplyChainImpactFinding struct {
	ReducerDomain         string           `json:"reducer_domain"`
	IntentID              string           `json:"intent_id"`
	ScopeID               string           `json:"scope_id"`
	GenerationID          string           `json:"generation_id"`
	SourceSystem          string           `json:"source_system"`
	Cause                 string           `json:"cause"`
	FindingID             string           `json:"finding_id"`
	CVEID                 string           `json:"cve_id"`
	AdvisoryID            string           `json:"advisory_id"`
	PackageID             string           `json:"package_id"`
	Ecosystem             string           `json:"ecosystem"`
	PackageName           string           `json:"package_name"`
	PURL                  string           `json:"purl"`
	ProductCriteria       string           `json:"product_criteria"`
	MatchCriteriaID       string           `json:"match_criteria_id"`
	ObservedVersion       string           `json:"observed_version"`
	RequestedRange        string           `json:"requested_range"`
	FixedVersion          string           `json:"fixed_version"`
	VulnerableRange       string           `json:"vulnerable_range"`
	MatchReason           string           `json:"match_reason"`
	ImpactStatus          string           `json:"impact_status"`
	Confidence            string           `json:"confidence"`
	CVSSScore             float64          `json:"cvss_score"`
	AdvisoryPublishedAt   string           `json:"advisory_published_at"`
	AdvisoryUpdatedAt     string           `json:"advisory_updated_at"`
	EPSSProbability       string           `json:"epss_probability"`
	EPSSPercentile        string           `json:"epss_percentile"`
	KnownExploited        bool             `json:"known_exploited"`
	PriorityReason        string           `json:"priority_reason"`
	PriorityScore         int              `json:"priority_score"`
	PriorityBucket        string           `json:"priority_bucket"`
	PriorityReasonCodes   []string         `json:"priority_reason_codes"`
	PriorityContributions []map[string]any `json:"priority_contributions"`
	RuntimeReachability   string           `json:"runtime_reachability"`
	RepositoryID          string           `json:"repository_id"`
	SubjectDigest         string           `json:"subject_digest"`
	ImageRef              string           `json:"image_ref"`
	DependencyScope       string           `json:"dependency_scope"`
	WorkloadIDs           []string         `json:"workload_ids"`
	DeploymentIDs         []string         `json:"deployment_ids"`
	ServiceIDs            []string         `json:"service_ids"`
	Environments          []string         `json:"environments"`
	CatalogEntityRefs     []string         `json:"catalog_entity_refs"`
	CatalogOwnerRefs      []string         `json:"catalog_owner_refs"`
	DetectionProfile      string           `json:"detection_profile"`
	MissingEvidence       []string         `json:"missing_evidence"`
	EvidencePath          []string         `json:"evidence_path"`
	EvidenceFactIDs       []string         `json:"evidence_fact_ids"`
	CanonicalWrites       int              `json:"canonical_writes"`
	SourceLayers          []string         `json:"source_layers"`
	DependencyPath        []string         `json:"dependency_path,omitempty"`
	DependencyDepth       *int             `json:"dependency_depth,omitempty"`
	DirectDependency      *bool            `json:"direct_dependency,omitempty"`
	Reachability          map[string]any   `json:"reachability,omitempty"`
	Provenance            map[string]any   `json:"provenance,omitempty"`
	Suppression           map[string]any   `json:"suppression,omitempty"`
	SuppressionState      *string          `json:"suppression_state,omitempty"`
	Remediation           map[string]any   `json:"remediation,omitempty"`
}

// AWSCloudRuntimeDriftFinding is the schema-version-1 payload for
// "reducer_aws_cloud_runtime_drift_finding". It is the AWS-specific reducer
// read model for cloud-only, state-only, ambiguous, and unknown resources.
type AWSCloudRuntimeDriftFinding struct {
	ReducerDomain                string           `json:"reducer_domain"`
	IntentID                     string           `json:"intent_id"`
	ScopeID                      string           `json:"scope_id"`
	GenerationID                 string           `json:"generation_id"`
	SourceSystem                 string           `json:"source_system"`
	Cause                        string           `json:"cause"`
	CanonicalID                  string           `json:"canonical_id"`
	CandidateID                  string           `json:"candidate_id"`
	CandidateKind                string           `json:"candidate_kind"`
	ARN                          string           `json:"arn"`
	FindingKind                  string           `json:"finding_kind"`
	ManagementStatus             string           `json:"management_status"`
	Confidence                   float64          `json:"confidence"`
	CandidateState               string           `json:"candidate_state"`
	MatchedTerraformStateAddress string           `json:"matched_terraform_state_address"`
	MissingEvidence              []string         `json:"missing_evidence"`
	WarningFlags                 []string         `json:"warning_flags"`
	RecommendedAction            string           `json:"recommended_action"`
	Evidence                     []map[string]any `json:"evidence"`
	OrphanedResources            int              `json:"orphaned_resources"`
	UnmanagedResources           int              `json:"unmanaged_resources"`
	AmbiguousResources           int              `json:"ambiguous_resources"`
	UnknownResources             int              `json:"unknown_resources"`
	PublicationFactKind          string           `json:"publication_fact_kind"`
	SourceLayers                 []string         `json:"source_layers"`
}

// TerraformConfigStateDriftFinding is the schema-version-1 payload for
// "reducer_terraform_config_state_drift_finding" (issue #5442). It carries two
// distinct row shapes distinguished by Outcome:
//
//   - Outcome "exact": one row per drifted Terraform resource address, the
//     durable form of the per-candidate telemetry the reducer already emits.
//     Address and DriftKind are populated; AmbiguousOwnerCandidates is empty.
//   - Outcome "ambiguous": one row per rejected state-snapshot scope where
//     backend-owner resolution found more than one candidate config repo
//     (tfstatebackend.ErrAmbiguousBackendOwner). Address and DriftKind are
//     empty — no per-address classification ran because no single anchor was
//     resolved — and AmbiguousOwnerCandidates carries every competing owner's
//     identity so the finding stays provenance-only (no repo is picked).
//
// "stale", "derived", "unresolved", and "rejected" are not emitted by this
// version: see go/internal/correlation/drift/tfconfigstate/doc.go for why each
// is either unreachable with the evidence this handler has today or
// intentionally not persisted.
type TerraformConfigStateDriftFinding struct {
	ReducerDomain string `json:"reducer_domain"`
	IntentID      string `json:"intent_id"`
	ScopeID       string `json:"scope_id"`
	GenerationID  string `json:"generation_id"`
	SourceSystem  string `json:"source_system"`
	Cause         string `json:"cause"`
	CanonicalID   string `json:"canonical_id"`
	CandidateID   string `json:"candidate_id"`
	CandidateKind string `json:"candidate_kind"`
	// Outcome is the closed join-confidence label: "exact" or "ambiguous".
	Outcome string `json:"outcome"`
	// Address is the Terraform resource address (e.g.
	// "module.app.aws_instance.web"). Empty for an "ambiguous" row.
	Address string `json:"address"`
	// DriftKind is one of the five tfconfigstate.DriftKind values. Empty for
	// an "ambiguous" row.
	DriftKind string `json:"drift_kind"`
	// BackendKind and LocatorHash identify the Terraform state backend the
	// finding was joined against (state_snapshot:<backend_kind>:<locator_hash>
	// scope shape).
	BackendKind string  `json:"backend_kind"`
	LocatorHash string  `json:"locator_hash"`
	Confidence  float64 `json:"confidence"`
	// AmbiguousOwnerCandidates carries the competing config-repo identities
	// for an "ambiguous" row (repo_id, scope_id, commit_id per candidate).
	// Always empty for an "exact" row.
	AmbiguousOwnerCandidates []map[string]any `json:"ambiguous_owner_candidates,omitempty"`
	Evidence                 []map[string]any `json:"evidence"`
	SourceLayers             []string         `json:"source_layers"`
}

// MultiCloudRuntimeDriftFinding is the schema-version-1 payload for
// "reducer_multi_cloud_runtime_drift_finding". It is the provider-neutral
// runtime drift read model keyed by canonical cloud_resource_uid.
type MultiCloudRuntimeDriftFinding struct {
	ReducerDomain                string           `json:"reducer_domain"`
	IntentID                     string           `json:"intent_id"`
	ScopeID                      string           `json:"scope_id"`
	GenerationID                 string           `json:"generation_id"`
	SourceSystem                 string           `json:"source_system"`
	Cause                        string           `json:"cause"`
	CanonicalID                  string           `json:"canonical_id"`
	CandidateID                  string           `json:"candidate_id"`
	CandidateKind                string           `json:"candidate_kind"`
	CloudResourceUID             string           `json:"cloud_resource_uid"`
	Provider                     string           `json:"provider"`
	RawIdentity                  string           `json:"raw_identity"`
	FindingKind                  string           `json:"finding_kind"`
	ManagementStatus             string           `json:"management_status"`
	Confidence                   float64          `json:"confidence"`
	CandidateState               string           `json:"candidate_state"`
	MatchedTerraformStateAddress string           `json:"matched_terraform_state_address"`
	MissingEvidence              []string         `json:"missing_evidence"`
	WarningFlags                 []string         `json:"warning_flags"`
	RecommendedAction            string           `json:"recommended_action"`
	Evidence                     []map[string]any `json:"evidence"`
	OrphanedResources            int              `json:"orphaned_resources"`
	UnmanagedResources           int              `json:"unmanaged_resources"`
	AmbiguousResources           int              `json:"ambiguous_resources"`
	UnknownResources             int              `json:"unknown_resources"`
	PublicationFactKind          string           `json:"publication_fact_kind"`
	SourceLayers                 []string         `json:"source_layers"`
}
