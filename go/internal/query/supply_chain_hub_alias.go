// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

// This file preserves the root package query surface cmd/api,
// cmd/mcp-server, staying stores, and staying tests still use for the
// supply-chain hub. The implementation moved to
// internal/query/supplychain (#6060 lane A); these aliases forward
// unchanged so those call sites compile without touching other lanes.
//
// The alias carries the handler's exported methods (Mount) and fields
// unchanged through the type alias below. Unexported hub helpers cannot
// cross this boundary: the shared ones are re-exported from the hub with
// root forwards at the bottom of this file, each naming its staying
// callers.

// SupplyChainHandler exposes reducer-owned supply-chain read models. See
// supplychain.SupplyChainHandler.
type SupplyChainHandler = supplychain.SupplyChainHandler

// SupplyChainImpactPacketResponder composes and writes the impact
// investigation packet. See supplychain.SupplyChainImpactPacketResponder.
type SupplyChainImpactPacketResponder = supplychain.SupplyChainImpactPacketResponder

// Container-image identity port and values. See the supplychain package.
type (
	ContainerImageIdentityStore              = supplychain.ContainerImageIdentityStore
	ContainerImageIdentityFilter             = supplychain.ContainerImageIdentityFilter
	ContainerImageIdentityRow                = supplychain.ContainerImageIdentityRow
	ContainerImageIdentityResult             = supplychain.ContainerImageIdentityResult
	ContainerImageIdentitySourceBridge       = supplychain.ContainerImageIdentitySourceBridge
	ContainerImageIdentityAggregateStore     = supplychain.ContainerImageIdentityAggregateStore
	ContainerImageIdentityAggregateFilter    = supplychain.ContainerImageIdentityAggregateFilter
	ContainerImageIdentityAggregateCount     = supplychain.ContainerImageIdentityAggregateCount
	ContainerImageIdentityInventoryRow       = supplychain.ContainerImageIdentityInventoryRow
	ContainerImageIdentityInventoryDimension = supplychain.ContainerImageIdentityInventoryDimension
)

// SBOM attestation attachment port and values. See the supplychain package.
type (
	SBOMAttestationAttachmentStore              = supplychain.SBOMAttestationAttachmentStore
	SBOMAttestationAttachmentFilter             = supplychain.SBOMAttestationAttachmentFilter
	SBOMAttestationAttachmentPage               = supplychain.SBOMAttestationAttachmentPage
	SBOMAttestationAttachmentRow                = supplychain.SBOMAttestationAttachmentRow
	SBOMAttestationAttachmentResult             = supplychain.SBOMAttestationAttachmentResult
	ComponentEvidenceRow                        = supplychain.ComponentEvidenceRow
	SLSAMaterialRow                             = supplychain.SLSAMaterialRow
	DependencyRelationshipRow                   = supplychain.DependencyRelationshipRow
	ExternalReferenceRow                        = supplychain.ExternalReferenceRow
	SBOMAttestationAttachmentAggregateStore     = supplychain.SBOMAttestationAttachmentAggregateStore
	SBOMAttestationAttachmentAggregateFilter    = supplychain.SBOMAttestationAttachmentAggregateFilter
	SBOMAttestationAttachmentAggregateCount     = supplychain.SBOMAttestationAttachmentAggregateCount
	SBOMAttestationAttachmentInventoryRow       = supplychain.SBOMAttestationAttachmentInventoryRow
	SBOMAttestationAttachmentInventoryDimension = supplychain.SBOMAttestationAttachmentInventoryDimension
)

// Security alert reconciliation port and values. See the supplychain package.
type (
	SecurityAlertReconciliationStore              = supplychain.SecurityAlertReconciliationStore
	SecurityAlertReconciliationFilter             = supplychain.SecurityAlertReconciliationFilter
	SecurityAlertReconciliationRow                = supplychain.SecurityAlertReconciliationRow
	SecurityAlertReconciliationResult             = supplychain.SecurityAlertReconciliationResult
	ProviderSecurityAlertRow                      = supplychain.ProviderSecurityAlertRow
	SecurityAlertEshuImpactRow                    = supplychain.SecurityAlertEshuImpactRow
	SecurityAlertEshuPackageRow                   = supplychain.SecurityAlertEshuPackageRow
	SecurityAlertMissingEvidence                  = supplychain.SecurityAlertMissingEvidence
	SecurityAlertReconciliationAggregateStore     = supplychain.SecurityAlertReconciliationAggregateStore
	SecurityAlertReconciliationAggregateFilter    = supplychain.SecurityAlertReconciliationAggregateFilter
	SecurityAlertReconciliationAggregateCount     = supplychain.SecurityAlertReconciliationAggregateCount
	SecurityAlertReconciliationInventoryRow       = supplychain.SecurityAlertReconciliationInventoryRow
	SecurityAlertReconciliationInventoryDimension = supplychain.SecurityAlertReconciliationInventoryDimension
)

// Runtime-evidence probe ports. See the supplychain package.
type (
	CloudResourceCurrentInventoryFilter      = supplychain.CloudResourceCurrentInventoryFilter
	CloudResourceRuntimeDigestResolver       = supplychain.CloudResourceRuntimeDigestResolver
	CloudResourceRuntimeDigestMatch          = supplychain.CloudResourceRuntimeDigestMatch
	KubernetesRuntimeCandidate               = supplychain.KubernetesRuntimeCandidate
	KubernetesRuntimeWorkloadMatch           = supplychain.KubernetesRuntimeWorkloadMatch
	KubernetesWorkloadCurrentInventoryFilter = supplychain.KubernetesWorkloadCurrentInventoryFilter
)

// Vulnerability suppression mutation port and values. See the supplychain
// package.
type (
	VulnerabilitySuppressionMutationStore    = supplychain.VulnerabilitySuppressionMutationStore
	VulnerabilitySuppressionMutationRequest  = supplychain.VulnerabilitySuppressionMutationRequest
	VulnerabilitySuppressionMutationResponse = supplychain.VulnerabilitySuppressionMutationResponse
)

// Capability constants. Registration stays in contract_supply_chain.go;
// the hub declares the values. See the supplychain package.
const (
	SBOMAttestationAttachmentsCapability             = supplychain.SBOMAttestationAttachmentsCapability
	VulnerabilityScannerReadContractCapability       = supplychain.VulnerabilityScannerReadContractCapability
	SupplyChainImpactFindingsCapability              = supplychain.SupplyChainImpactFindingsCapability
	SupplyChainImpactExplanationCapability           = supplychain.SupplyChainImpactExplanationCapability
	ContainerImageIdentitiesCapability               = supplychain.ContainerImageIdentitiesCapability
	SecurityAlertReconciliationsCapability           = supplychain.SecurityAlertReconciliationsCapability
	SupplyChainImpactAggregateCapability             = supplychain.SupplyChainImpactAggregateCapability
	SecurityAlertReconciliationAggregateCapability   = supplychain.SecurityAlertReconciliationAggregateCapability
	ContainerImageIdentityAggregateCapability        = supplychain.ContainerImageIdentityAggregateCapability
	SBOMAttestationAttachmentAggregateCapability     = supplychain.SBOMAttestationAttachmentAggregateCapability
	SecurityAlertReconciliationAnchorRequiredMessage = supplychain.SecurityAlertReconciliationAnchorRequiredMessage
)

// Limit and probe-budget constants. The staying stores and staying tests
// bound through these; the hub declares the values. See the supplychain
// package.
const (
	SBOMAttestationAttachmentMaxLimit                       = supplychain.SBOMAttestationAttachmentMaxLimit
	ContainerImageIdentityMaxLimit                          = supplychain.ContainerImageIdentityMaxLimit
	SecurityAlertReconciliationMaxLimit                     = supplychain.SecurityAlertReconciliationMaxLimit
	ContainerImageIdentityAggregateMaxLimit                 = supplychain.ContainerImageIdentityAggregateMaxLimit
	SBOMAttestationAttachmentAggregateMaxLimit              = supplychain.SBOMAttestationAttachmentAggregateMaxLimit
	SecurityAlertReconciliationAggregateMaxLimit            = supplychain.SecurityAlertReconciliationAggregateMaxLimit
	SBOMAttestationWarningSummaryPreviewMaxCount            = supplychain.SBOMAttestationWarningSummaryPreviewMaxCount
	SupplyChainCloudRuntimeProbeMaxResults                  = supplychain.SupplyChainCloudRuntimeProbeMaxResults
	SupplyChainCloudRuntimeProbeMaxDigests                  = supplychain.SupplyChainCloudRuntimeProbeMaxDigests
	SupplyChainKubernetesRuntimeProbeMaxResults             = supplychain.SupplyChainKubernetesRuntimeProbeMaxResults
	SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates = supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates
	SupplyChainKubernetesRuntimeProbeCypher                 = supplychain.SupplyChainKubernetesRuntimeProbeCypher
	ContainerImageIdentityInventoryByOutcome                = supplychain.ContainerImageIdentityInventoryByOutcome
	ContainerImageIdentityInventoryByIdentityStrength       = supplychain.ContainerImageIdentityInventoryByIdentityStrength
	ContainerImageIdentityInventoryByRepository             = supplychain.ContainerImageIdentityInventoryByRepository
	SBOMAttestationAttachmentInventoryByAttachmentStatus    = supplychain.SBOMAttestationAttachmentInventoryByAttachmentStatus
	SBOMAttestationAttachmentInventoryByArtifactKind        = supplychain.SBOMAttestationAttachmentInventoryByArtifactKind
	SBOMAttestationAttachmentInventoryBySubjectDigest       = supplychain.SBOMAttestationAttachmentInventoryBySubjectDigest
	SecurityAlertReconciliationInventoryByStatus            = supplychain.SecurityAlertReconciliationInventoryByStatus
	SecurityAlertReconciliationInventoryByProvider          = supplychain.SecurityAlertReconciliationInventoryByProvider
	SecurityAlertReconciliationInventoryByProviderState     = supplychain.SecurityAlertReconciliationInventoryByProviderState
	SecurityAlertReconciliationInventoryByRepository        = supplychain.SecurityAlertReconciliationInventoryByRepository
	SecurityAlertReconciliationInventoryByPackage           = supplychain.SecurityAlertReconciliationInventoryByPackage
)

// Unexported forwards for staying root files. The hub declares the values
// under exported names; the staying contract matrix, staying stores, and
// staying tests keep their exact pre-move spelling through these. Each
// names its staying callers; hub-internal callers use the exported hub
// names directly.
const (
	vulnerabilityScannerReadContractCapability = supplychain.VulnerabilityScannerReadContractCapability
	sbomAttestationAttachmentsCapability       = supplychain.SBOMAttestationAttachmentsCapability
	supplyChainImpactFindingsCapability        = supplychain.SupplyChainImpactFindingsCapability
	supplyChainImpactExplanationCapability     = supplychain.SupplyChainImpactExplanationCapability
	containerImageIdentitiesCapability         = supplychain.ContainerImageIdentitiesCapability
	securityAlertReconciliationsCapability     = supplychain.SecurityAlertReconciliationsCapability
	supplyChainImpactAggregateCapability       = supplychain.SupplyChainImpactAggregateCapability
	// Staying callers: contract_supply_chain.go capability matrix.
	securityAlertReconciliationAggregateCapability = supplychain.SecurityAlertReconciliationAggregateCapability
	containerImageIdentityAggregateCapability      = supplychain.ContainerImageIdentityAggregateCapability
	sbomAttestationAttachmentAggregateCapability   = supplychain.SBOMAttestationAttachmentAggregateCapability

	sbomAttestationAttachmentMaxLimit   = supplychain.SBOMAttestationAttachmentMaxLimit
	containerImageIdentityMaxLimit      = supplychain.ContainerImageIdentityMaxLimit
	securityAlertReconciliationMaxLimit = supplychain.SecurityAlertReconciliationMaxLimit
	// Staying callers: the Postgres store limit checks.

	supplyChainCloudRuntimeProbeMaxResults = supplychain.SupplyChainCloudRuntimeProbeMaxResults
	// Staying callers: cloud_resource_list_store.go owner-ledger budget.
	supplyChainKubernetesRuntimeProbeMaxResults             = supplychain.SupplyChainKubernetesRuntimeProbeMaxResults
	supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates = supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates
	// Staying callers: kubernetes_runtime_workload_store.go candidate budget.
	supplyChainKubernetesRuntimeProbeMaxConcurrency = supplychain.SupplyChainKubernetesRuntimeProbeMaxConcurrency
	supplyChainKubernetesRuntimeProbeCypher         = supplychain.SupplyChainKubernetesRuntimeProbeCypher
	// Staying callers: queryplan_production_binding_test.go, which pins
	// the exact Cypher. (The probe unit/perf tests moved to the hub and
	// use the exported hub name directly, as does the digest-starvation
	// live test for the per-digest floor, so the fan-out-bound and
	// per-digest-floor forwards are deleted; the candidate-cap and plan
	// forwards for the moved runtime-context tests are deleted too.)
	supplyChainImpactFindingMaxLimit = supplychain.SupplyChainImpactFindingMaxLimit
	// Staying callers: the findings limit tests, which pin the page bound.
	supplyChainKubernetesRuntimeEvidenceSource = supplychain.SupplyChainKubernetesRuntimeEvidenceSource
	supplyChainKubernetesRuntimeResolutionMode = supplychain.SupplyChainKubernetesRuntimeResolutionMode
	// Staying callers: queryplan_profile_params_test.go, which pins the
	// probe's evidence-source contract.

	securityAlertReconciliationAnchorRequiredMessage = supplychain.SecurityAlertReconciliationAnchorRequiredMessage
	// Staying callers: security_alert_reconciliation.go store error.
	sbomAttestationWarningSummaryPreviewMaxCount = supplychain.SBOMAttestationWarningSummaryPreviewMaxCount
	// Staying callers: sbom_attestation_attachment_rows.go decode wrappers.
)

// Shared seams the staying files reuse. Each names its staying callers;
// hub-internal callers use the exported hub names directly.

// uniqueSortedNonEmpty trims, drops empties, dedupes, and sorts. Staying
// callers: ci_cd_evidence_summary.go, sbom_attestation_attachments.go, and
// staying tests. See supplychain.UniqueSortedNonEmpty.
func uniqueSortedNonEmpty(values []string) []string {
	return supplychain.UniqueSortedNonEmpty(values)
}

// securityAlertRepositoryScopeIDs prepends the repository id to the scope
// set, trimmed, deduped, and sorted. Staying callers:
// security_alert_reconciliation.go,
// security_alert_reconciliation_aggregates.go. See
// supplychain.SecurityAlertRepositoryScopeIDs.
func securityAlertRepositoryScopeIDs(repositoryID string, scopeIDs []string) []string {
	return supplychain.SecurityAlertRepositoryScopeIDs(repositoryID, scopeIDs)
}

// supplyChainCloudRuntimeProbePerDigestLimit shares the owner-ledger row
// budget across a page's digests. Staying callers:
// cloud_resource_list_store.go and staying cloud tests. See
// supplychain.SupplyChainCloudRuntimeProbePerDigestLimit.
func supplyChainCloudRuntimeProbePerDigestLimit(digestCount int) int {
	return supplychain.SupplyChainCloudRuntimeProbePerDigestLimit(digestCount)
}

// boundedSBOMWarningSummaries bounds one attachment's warning summaries.
// Staying callers: sbom_attestation_attachment_rows.go decode wrappers. See
// supplychain.BoundedSBOMWarningSummaries.
func boundedSBOMWarningSummaries(values []string) ([]string, int, bool) {
	return supplychain.BoundedSBOMWarningSummaries(values)
}

// buildContainerImageIdentitySourceBridge summarizes source-repository
// evidence for the bridge test. Staying callers:
// container_image_identities_source_bridge_test.go. See
// supplychain.BuildContainerImageIdentitySourceBridge.
func buildContainerImageIdentitySourceBridge(
	sourceRepositoryID string,
	rows []ContainerImageIdentityResult,
) ContainerImageIdentitySourceBridge {
	return supplychain.BuildContainerImageIdentitySourceBridge(sourceRepositoryID, rows)
}

// Aggregate pagination and scope helpers the staying aggregate tests pin.
// Each forwards to the hub implementation the handlers call; see the
// supplychain package.
func nextContainerImageIdentityAggregateOffset(offset, limit int, truncated bool) any {
	return supplychain.NextContainerImageIdentityAggregateOffset(offset, limit, truncated)
}

func nextSBOMAttestationAttachmentAggregateOffset(offset, limit int, truncated bool) any {
	return supplychain.NextSBOMAttestationAttachmentAggregateOffset(offset, limit, truncated)
}

func nextSecurityAlertReconciliationAggregateOffset(offset, limit int, truncated bool) any {
	return supplychain.NextSecurityAlertReconciliationAggregateOffset(offset, limit, truncated)
}

func nextSupplyChainImpactAggregateOffset(offset, limit int, truncated bool) any {
	return supplychain.NextSupplyChainImpactAggregateOffset(offset, limit, truncated)
}

// sbomAttestationAttachmentAggregateScope builds the scope envelope the
// staying SBOM aggregate tests pin. See
// supplychain.SBOMAttestationAttachmentAggregateScope.
func sbomAttestationAttachmentAggregateScope(filter SBOMAttestationAttachmentAggregateFilter) map[string]string {
	return supplychain.SBOMAttestationAttachmentAggregateScope(filter)
}

// SupplyChainRuntimeEnvironmentPlan is one finding's runtime-environment
// probe plan. See supplychain.SupplyChainRuntimeEnvironmentPlan.
type SupplyChainRuntimeEnvironmentPlan = supplychain.SupplyChainRuntimeEnvironmentPlan

// The staying Postgres implementations satisfy the hub ports through these
// assertions: wiring assigns the concrete stores to hub-typed handler
// fields, and any port drift fails here rather than at a call site.
var (
	_ ContainerImageIdentityStore               = PostgresContainerImageIdentityStore{}
	_ ContainerImageIdentityAggregateStore      = PostgresContainerImageIdentityAggregateStore{}
	_ SBOMAttestationAttachmentStore            = PostgresSBOMAttestationAttachmentStore{}
	_ SBOMAttestationAttachmentAggregateStore   = PostgresSBOMAttestationAttachmentAggregateStore{}
	_ SecurityAlertReconciliationStore          = PostgresSecurityAlertReconciliationStore{}
	_ SecurityAlertReconciliationAggregateStore = PostgresSecurityAlertReconciliationAggregateStore{}
	_ CloudResourceCurrentInventoryFilter       = (*PostgresCloudResourceListStore)(nil)
	_ CloudResourceRuntimeDigestResolver        = (*PostgresCloudResourceListStore)(nil)
	_ KubernetesWorkloadCurrentInventoryFilter  = (*PostgresKubernetesRuntimeWorkloadStore)(nil)
)
