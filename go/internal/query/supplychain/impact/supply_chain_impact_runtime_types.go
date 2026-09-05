// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Runtime-evidence read-model types relocated from root package query
// (#6060 lane A). The declarations moved here byte-identical because the
// moved finding rows and runtime-context store name them and this package
// must not import root; the staying probe files keep `type X = impact.X`
// aliases (see root supply_chain_impact_alias.go) so handler code, tests,
// and cmd wiring keep compiling unchanged. The probes that populate these
// types stay in root until the hub PR3 (they drive off SupplyChainHandler
// and, for the fair probe, root GraphQuery).
//
// Relocated declarations and their root sources:
//   - SupplyChainRuntimeContext, SupplyChainRuntimeContextResult,
//     SupplyChainRuntimeEnvironmentEvidenceProbe,
//     SupplyChainRuntimeEnvironmentCandidate: from
//     supply_chain_impact_runtime_context_probe.go.
//   - KubernetesRuntimeWorkloadRef: from
//     supply_chain_impact_kubernetes_runtime_probe.go.
//   - KubernetesRuntimeProbeMetadata: from
//     supply_chain_impact_kubernetes_runtime_probe_fair.go.

// SupplyChainRuntimeContext is one repository's read-time-resolved runtime
// context: the workloads, services, deployments, environments, and catalog
// refs that repository currently maps to, resolved from active
// workload_identity, service_catalog_correlation, platform_materialization,
// and deployment-correlation facts at query time (issue #5746).
//
// Read-time resolution is deliberate: a finding's baked payload fields are
// stamped at reduce time and go stale the moment runtime reality changes
// (redeploy, delete, promote), while this join reads the CURRENT facts on
// every request. Absence of a workload here is an honest "current state of
// knowledge" that self-heals on the next read — no readiness gate, no
// re-enqueue, no fan-out.
type SupplyChainRuntimeContext struct {
	WorkloadIDs       []string
	ServiceIDs        []string
	DeploymentIDs     []string
	Environments      []string
	CatalogEntityRefs []string
	CatalogOwnerRefs  []string
}

// SupplyChainRuntimeContextResult is the response-side envelope attached to
// one impact finding as `runtime_context` (#5746). TruthBasis labels the
// resolution path so a caller cannot mistake these IDs for baked payload
// fields. The workload_id/service_id/environment filters resolve the same
// current repository mappings independently (#5747).
type SupplyChainRuntimeContextResult struct {
	// TruthBasis is always "read_time_resolved": the context was resolved
	// from the repository's active runtime facts at query time, not baked
	// into the finding at reduce time. Empty lists are an honest "no runtime
	// facts landed yet" (fresh ingest) that self-heals on the next read.
	TruthBasis    string   `json:"truth_basis"`
	WorkloadIDs   []string `json:"workload_ids,omitempty"`
	ServiceIDs    []string `json:"service_ids,omitempty"`
	DeploymentIDs []string `json:"deployment_ids,omitempty"`
	Environments  []string `json:"environments,omitempty"`
	// EnvironmentEvidence records the strongest current corroboration state for
	// each admitted exact (subject_digest, environment) lookup. Repository
	// context contributes candidate names only; it cannot supply or default an
	// evidence value. Values use the existing deploy_event/declared vocabulary.
	EnvironmentEvidence map[string]string `json:"environment_evidence,omitempty"`
	// EnvironmentEvidenceProbe reports this finding's page-weighted current
	// confirmation budget. CandidatesTruncated means visible candidate names
	// exceeded that budget; it never reflects hidden or unauthorized facts.
	EnvironmentEvidenceProbe *SupplyChainRuntimeEnvironmentEvidenceProbe `json:"environment_evidence_probe,omitempty"`
	CatalogEntityRefs        []string                                    `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs         []string                                    `json:"catalog_owner_refs,omitempty"`
}

// SupplyChainRuntimeEnvironmentEvidenceProbe describes the bounded current
// confirmation work performed for one finding's environment candidates.
type SupplyChainRuntimeEnvironmentEvidenceProbe struct {
	CandidateLimit      int  `json:"candidate_limit"`
	CandidatesTruncated bool `json:"candidates_truncated"`
}

// KubernetesRuntimeWorkloadRef is one current, authorized Kubernetes workload
// observed running the parent finding's exact subject digest.
type KubernetesRuntimeWorkloadRef struct {
	UID       string `json:"workload_uid"`
	ClusterID string `json:"cluster_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// KubernetesRuntimeProbeMetadata describes the bounded, page-weighted
// digest-local candidate budget. A nil WorkloadRefsTruncated means
// authorization prevents the caller from learning whether hidden candidates
// exist.
type KubernetesRuntimeProbeMetadata struct {
	CandidateLimit        int   `json:"candidate_limit"`
	WorkloadRefsTruncated *bool `json:"workload_refs_truncated"`
}

// SupplyChainRuntimeEnvironmentCandidate identifies one finding-bound
// digest/environment pair that must be revalidated against current accepted
// CI/CD correlation facts before it can enter read-time runtime_context.
// Relocated from root package query's
// supply_chain_impact_runtime_context_probe.go (#6060 lane A): the moved
// runtime-environment store names it and this package must not import root,
// so the declaration lives here and root keeps a `type X = impact.X` alias
// (see root supply_chain_impact_alias.go).
type SupplyChainRuntimeEnvironmentCandidate struct {
	SubjectDigest string
	Environment   string
}
