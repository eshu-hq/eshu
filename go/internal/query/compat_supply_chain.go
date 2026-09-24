// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// compat_supply_chain.go merges the three pre-#6642 root alias files
// (supply_chain_hub_alias.go, supply_chain_advisory_alias.go,
// supply_chain_impact_alias.go) into one compat bucket so no root file's name
// collides with the new go/internal/query/supply subpackage (dirgate's
// sibling-word naming rule). Only the right-hand sides below point at the new
// supply/chain paths; every left-hand alias spelling is unchanged, so
// cmd/api, cmd/mcp-server, the staying supply-chain handlers/probes/tests,
// internal/serviceintelhttp, internal/cli, and internal/storage tests keep
// compiling unchanged. Three labeled sections below match the three files
// this replaces.

// === Hub aliases (formerly supply_chain_hub_alias.go) ===

// This section preserves the root package query surface cmd/api,
// cmd/mcp-server, staying stores, and staying tests still use for the
// supply-chain hub. The implementation moved to
// internal/query/supply/chain (#6060 lane A); these aliases forward
// unchanged so those call sites compile without touching other lanes.
//
// The alias carries the handler's exported methods (Mount) and fields
// unchanged through the type alias below. Unexported hub helpers cannot
// cross this boundary: the shared ones are re-exported from the hub with
// root forwards at the bottom of this file, each naming its staying
// callers.

// SupplyChainHandler exposes reducer-owned supply-chain read models. See
// supplychain.Handler.
type SupplyChainHandler = supplychain.Handler

// SupplyChainImpactPacketResponder composes and writes the impact
// investigation packet. See supplychain.ImpactPacketResponder.
type SupplyChainImpactPacketResponder = supplychain.ImpactPacketResponder

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
	SupplyChainImpactFindingsCapability              = supplychain.ImpactFindingsCapability
	SupplyChainImpactExplanationCapability           = supplychain.ImpactExplanationCapability
	ContainerImageIdentitiesCapability               = supplychain.ContainerImageIdentitiesCapability
	SecurityAlertReconciliationsCapability           = supplychain.SecurityAlertReconciliationsCapability
	SupplyChainImpactAggregateCapability             = supplychain.ImpactAggregateCapability
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
	SupplyChainCloudRuntimeProbeMaxResults                  = supplychain.CloudRuntimeProbeMaxResults
	SupplyChainCloudRuntimeProbeMaxDigests                  = supplychain.CloudRuntimeProbeMaxDigests
	SupplyChainKubernetesRuntimeProbeMaxResults             = supplychain.KubernetesRuntimeProbeMaxResults
	SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates = supplychain.KubernetesRuntimeProbeMaxAllScopesCandidates
	SupplyChainKubernetesRuntimeProbeCypher                 = supplychain.KubernetesRuntimeProbeCypher
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
	supplyChainImpactFindingsCapability        = supplychain.ImpactFindingsCapability
	supplyChainImpactExplanationCapability     = supplychain.ImpactExplanationCapability
	containerImageIdentitiesCapability         = supplychain.ContainerImageIdentitiesCapability
	securityAlertReconciliationsCapability     = supplychain.SecurityAlertReconciliationsCapability
	supplyChainImpactAggregateCapability       = supplychain.ImpactAggregateCapability
	// Staying callers: contract_supply_chain.go capability matrix.

	sbomAttestationAttachmentMaxLimit = supplychain.SBOMAttestationAttachmentMaxLimit
	containerImageIdentityMaxLimit    = supplychain.ContainerImageIdentityMaxLimit
	// Staying callers: the Postgres store limit checks.

	supplyChainCloudRuntimeProbeMaxResults = supplychain.CloudRuntimeProbeMaxResults
	// Staying callers: cloud_resource_list_store.go owner-ledger budget.
	supplyChainKubernetesRuntimeProbeMaxConcurrency = supplychain.KubernetesRuntimeProbeMaxConcurrency
	supplyChainKubernetesRuntimeProbeCypher         = supplychain.KubernetesRuntimeProbeCypher
	// Staying callers: queryplan_production_binding_test.go, which pins
	// the exact Cypher. (The probe unit/perf tests moved to the hub and
	// use the exported hub name directly, as does the digest-starvation
	// live test for the per-digest floor, so the fan-out-bound and
	// per-digest-floor forwards are deleted; the candidate-cap and plan
	// forwards for the moved runtime-context tests are deleted too.)
	supplyChainImpactFindingMaxLimit = supplychain.ImpactFindingMaxLimit
	// Staying callers: the findings limit tests, which pin the page bound.
	supplyChainKubernetesRuntimeEvidenceSource = supplychain.KubernetesRuntimeEvidenceSource
	supplyChainKubernetesRuntimeResolutionMode = supplychain.KubernetesRuntimeResolutionMode
	// Staying callers: queryplan_profile_params_test.go, which pins the
	// probe's evidence-source contract.

	sbomAttestationWarningSummaryPreviewMaxCount = supplychain.SBOMAttestationWarningSummaryPreviewMaxCount
	// Staying callers: sbom_attestation_attachment_rows.go decode wrappers.
)

// stringMapVal stringifies a map payload field. Its home is
// supply/chain/alerts/ (StringMapVal); this forward keeps
// sbom_attestation_attachments.go and sbom_attestation_attachment_rows.go
// spelling the unqualified name unchanged. See #6642.
func stringMapVal(payload map[string]any, key string) map[string]string {
	return alerts.StringMapVal(payload, key)
}

// PostgresSecurityAlertReconciliationStore reads active provider alert
// reconciliation facts from Postgres. Its home is supply/chain/alerts/
// (PostgresStore); this alias keeps the cmd/api and cmd/mcp-server wiring
// spelling query.PostgresSecurityAlertReconciliationStore unchanged. See
// #6642.
type PostgresSecurityAlertReconciliationStore = alerts.PostgresStore

// NewPostgresSecurityAlertReconciliationStore creates the Postgres-backed
// provider alert reconciliation read model. Its home is supply/chain/alerts/
// (NewPostgresStore); this forwarder keeps cmd/api and cmd/mcp-server wiring
// calling query.NewPostgresSecurityAlertReconciliationStore unchanged, passing
// the *sql.DB main always passed (it satisfies alerts.Queryer). See #6642.
func NewPostgresSecurityAlertReconciliationStore(db alerts.Queryer) PostgresSecurityAlertReconciliationStore {
	return alerts.NewPostgresStore(db)
}

// PostgresSecurityAlertReconciliationAggregateStore reads aggregate counts
// directly from reducer-owned reconciliation facts. Its home is
// supply/chain/alerts/ (PostgresAggregateStore); this alias keeps the cmd/api
// and cmd/mcp-server wiring spelling
// query.PostgresSecurityAlertReconciliationAggregateStore unchanged. See
// #6642.
type PostgresSecurityAlertReconciliationAggregateStore = alerts.PostgresAggregateStore

// NewPostgresSecurityAlertReconciliationAggregateStore creates the
// Postgres-backed aggregate store. Its home is supply/chain/alerts/
// (NewPostgresAggregateStore); this forwarder keeps cmd/api and
// cmd/mcp-server wiring calling
// query.NewPostgresSecurityAlertReconciliationAggregateStore unchanged,
// passing the *sql.DB main always passed (it satisfies
// alerts.AggregateQueryer). See #6642.
func NewPostgresSecurityAlertReconciliationAggregateStore(db alerts.AggregateQueryer) PostgresSecurityAlertReconciliationAggregateStore {
	return alerts.NewPostgresAggregateStore(db)
}

// Shared seams the staying files reuse. Each names its staying callers;
// hub-internal callers use the exported hub names directly.

// uniqueSortedNonEmpty trims, drops empties, dedupes, and sorts. Staying
// callers: ci_cd_evidence_summary.go, sbom_attestation_attachments.go, and
// staying tests. See supplychain.UniqueSortedNonEmpty.
func uniqueSortedNonEmpty(values []string) []string {
	return supplychain.UniqueSortedNonEmpty(values)
}

// supplyChainCloudRuntimeProbePerDigestLimit shares the owner-ledger row
// budget across a page's digests. Staying callers:
// cloud_resource_list_store.go and staying cloud tests. See
// supplychain.CloudRuntimeProbePerDigestLimit.
func supplyChainCloudRuntimeProbePerDigestLimit(digestCount int) int {
	return supplychain.CloudRuntimeProbePerDigestLimit(digestCount)
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
	return supplychain.NextImpactAggregateOffset(offset, limit, truncated)
}

// sbomAttestationAttachmentAggregateScope builds the scope envelope the
// staying SBOM aggregate tests pin. See
// supplychain.SBOMAttestationAttachmentAggregateScope.
func sbomAttestationAttachmentAggregateScope(filter SBOMAttestationAttachmentAggregateFilter) map[string]string {
	return supplychain.SBOMAttestationAttachmentAggregateScope(filter)
}

// SupplyChainRuntimeEnvironmentPlan is one finding's runtime-environment
// probe plan. See supplychain.RuntimeEnvironmentPlan.
type SupplyChainRuntimeEnvironmentPlan = supplychain.RuntimeEnvironmentPlan

// The staying Postgres implementations satisfy the hub ports through these
// assertions: wiring assigns the concrete stores to hub-typed handler
// fields, and any port drift fails here rather than at a call site.
var (
	_ ContainerImageIdentityStore              = PostgresContainerImageIdentityStore{}
	_ ContainerImageIdentityAggregateStore     = PostgresContainerImageIdentityAggregateStore{}
	_ SBOMAttestationAttachmentStore           = PostgresSBOMAttestationAttachmentStore{}
	_ SBOMAttestationAttachmentAggregateStore  = PostgresSBOMAttestationAttachmentAggregateStore{}
	_ CloudResourceCurrentInventoryFilter      = (*PostgresCloudResourceListStore)(nil)
	_ CloudResourceRuntimeDigestResolver       = (*PostgresCloudResourceListStore)(nil)
	_ KubernetesWorkloadCurrentInventoryFilter = (*PostgresKubernetesRuntimeWorkloadStore)(nil)
)

// === Advisory aliases (formerly supply_chain_advisory_alias.go) ===

// This section preserves the root package query surface cmd/api and
// cmd/mcp-server still use for the advisory read models. The implementation
// moved to internal/query/supply/chain/advisory (#6060 lane A); these aliases
// forward unchanged so the wiring call sites compile without touching other
// lanes. The hub PR3 owns the final alias surface for this family (handler
// aliases join here when the handlers move); keep this file to the
// constructor-level compatibility cmd/* needs until then.

// PostgresAdvisoryCatalogStore reads a bounded, browsable page of canonical
// vulnerability advisories. See advisory.PostgresCatalogStore.
type PostgresAdvisoryCatalogStore = advisory.PostgresCatalogStore

// PostgresAdvisoryEvidenceStore reads active vulnerability source facts and
// groups them into canonical advisory evidence rows. See
// advisory.PostgresEvidenceStore.
type PostgresAdvisoryEvidenceStore = advisory.PostgresEvidenceStore

// NewPostgresAdvisoryCatalogStore constructs the Postgres-backed catalog
// read model. Forwards unchanged to
// advisory.NewPostgresCatalogStore.
func NewPostgresAdvisoryCatalogStore(db advisory.EvidenceQueryer) PostgresAdvisoryCatalogStore {
	return advisory.NewPostgresCatalogStore(db)
}

// NewPostgresAdvisoryEvidenceStore constructs the Postgres-backed advisory
// evidence read model. Forwards unchanged to
// advisory.NewPostgresEvidenceStore.
func NewPostgresAdvisoryEvidenceStore(db advisory.EvidenceQueryer) PostgresAdvisoryEvidenceStore {
	return advisory.NewPostgresEvidenceStore(db)
}

// listAdvisoryCatalogQuery and listAdvisoryEvidenceQuery re-expose the
// advisory SQL shapes under their pre-move bare names for the staying root
// tests. The gocritic argOrder heuristic misfires on the qualified
// advisory.X form inside strings.Contains assertions (a bare identifier of
// the same name passes, as the container-image query tests show), so the
// tests keep the exact pre-move call shape through these shims. Both go
// away in hub PR3 when the tests move into the advisory package with the
// handlers they drive.
var (
	listAdvisoryCatalogQuery  = advisory.ListCatalogQuery
	listAdvisoryEvidenceQuery = advisory.ListEvidenceQuery
)

// === Impact aliases (formerly supply_chain_impact_alias.go) ===

// This section preserves the root package query surface cmd/api,
// cmd/mcp-server, the staying supply-chain handlers, probes, and tests still
// use for the impact read-model types. The declarations moved to
// internal/query/supply/chain/impact (#6060 lane A); these aliases forward
// unchanged so the staying call sites compile without touching other lanes.
// The hub PR3 owns the final alias surface for this family (store and
// constructor aliases join here when the handlers move); keep this file to
// the type-level compatibility the staying probes and rows need until then.

// SupplyChainRuntimeContext is one repository's read-time-resolved runtime
// context. See impact.RuntimeContext.
type SupplyChainRuntimeContext = impact.RuntimeContext

// SupplyChainRuntimeContextResult is the response-side runtime-context
// envelope attached to one impact finding. See
// impact.RuntimeContextResult.
type SupplyChainRuntimeContextResult = impact.RuntimeContextResult

// SupplyChainRuntimeEnvironmentEvidenceProbe describes the bounded current
// confirmation work for one finding's environment candidates. See
// impact.RuntimeEnvironmentEvidenceProbe.
type SupplyChainRuntimeEnvironmentEvidenceProbe = impact.RuntimeEnvironmentEvidenceProbe

// KubernetesRuntimeWorkloadRef is one current, authorized Kubernetes workload
// observed running a finding's exact subject digest. See
// impact.KubernetesRuntimeWorkloadRef.
type KubernetesRuntimeWorkloadRef = impact.KubernetesRuntimeWorkloadRef

// KubernetesRuntimeProbeMetadata describes the bounded, page-weighted
// digest-local candidate budget. See impact.KubernetesRuntimeProbeMetadata.
type KubernetesRuntimeProbeMetadata = impact.KubernetesRuntimeProbeMetadata

// SupplyChainImpactProfilePrecise selects exact installed-version
// anchored findings only. See impact.ProfilePrecise.
const SupplyChainImpactProfilePrecise = impact.ProfilePrecise

// SupplyChainImpactProfileComprehensive selects every owned-anchor
// finding including range-only manifest, SBOM/CPE-derived,
// malformed range, and missing-version rows. See
// impact.ProfileComprehensive.
const SupplyChainImpactProfileComprehensive = impact.ProfileComprehensive

// SupplyChainRuntimeEnvironmentCandidate identifies one finding-bound
// digest/environment pair that must be revalidated against current accepted
// CI/CD correlation facts before it can enter read-time runtime_context.
// See impact.RuntimeEnvironmentCandidate.
type SupplyChainRuntimeEnvironmentCandidate = impact.RuntimeEnvironmentCandidate

// VulnerabilitySuppressionMutationResult identifies the durable generation
// containing an operator suppression. See
// impact.VulnerabilitySuppressionMutationResult.
type VulnerabilitySuppressionMutationResult = impact.VulnerabilitySuppressionMutationResult

// The remaining aliases preserve the root package query surface cmd/api,
// cmd/mcp-server, internal/serviceintelhttp, internal/cli, and
// internal/storage tests still use for the impact read models. The
// declarations moved to internal/query/supply/chain/impact (#6060 lane A);
// these aliases forward unchanged so those call sites compile without
// touching other lanes.

type (
	SupplyChainImpactAggregateCount               = impact.AggregateCount
	SupplyChainImpactAggregateFilter              = impact.AggregateFilter
	SupplyChainImpactAggregateStore               = impact.AggregateStore
	SupplyChainImpactEvidenceFactSummary          = impact.EvidenceFactSummary
	SupplyChainImpactExplanationAnchors           = impact.ExplanationAnchors
	SupplyChainImpactExplanationFilter            = impact.ExplanationFilter
	SupplyChainImpactExplanationFreshness         = impact.ExplanationFreshness
	SupplyChainImpactExplanationResult            = impact.ExplanationResult
	SupplyChainImpactFindingFilter                = impact.FindingFilter
	SupplyChainImpactFindingResult                = impact.FindingResult
	SupplyChainImpactFindingRow                   = impact.FindingRow
	SupplyChainImpactFindingStore                 = impact.FindingStore
	SupplyChainImpactInventoryDimension           = impact.InventoryDimension
	SupplyChainImpactInventoryRow                 = impact.InventoryRow
	SupplyChainImpactPathHop                      = impact.PathHop
	SupplyChainImpactReadinessEnvelope            = impact.ReadinessEnvelope
	PostgresSupplyChainImpactFindingStore         = impact.PostgresFindingStore
	PostgresSupplyChainImpactAggregateStore       = impact.PostgresAggregateStore
	PostgresSupplyChainImpactReadinessStore       = impact.PostgresReadinessStore
	PostgresVulnerabilitySuppressionMutationStore = impact.PostgresVulnerabilitySuppressionMutationStore
)

const (
	SupplyChainImpactAggregateMaxLimit       = impact.AggregateMaxLimit
	ReadinessStateReadyWithFindings          = impact.ReadinessStateReadyWithFindings
	SupplyChainImpactInventoryByImpactStatus = impact.InventoryByStatus
	SupplyChainImpactWinnersReadEnv          = impact.WinnersReadEnv
)

func SupplyChainImpactWinnersReadEnabled(value string) bool {
	return impact.WinnersReadEnabled(value)
}

func NewPostgresSupplyChainImpactFindingStore(db impact.FindingQueryer) PostgresSupplyChainImpactFindingStore {
	return impact.NewPostgresFindingStore(db)
}

func NewPostgresSupplyChainImpactFindingStoreWithReadModel(db impact.FindingQueryer, readFromWinners bool) PostgresSupplyChainImpactFindingStore {
	return impact.NewPostgresFindingStoreWithReadModel(db, readFromWinners)
}

func NewPostgresSupplyChainImpactAggregateStore(db impact.AggregateQueryer) PostgresSupplyChainImpactAggregateStore {
	return impact.NewPostgresAggregateStore(db)
}

func NewPostgresSupplyChainImpactReadinessStore(db impact.ReadinessQueryer) PostgresSupplyChainImpactReadinessStore {
	return impact.NewPostgresReadinessStore(db)
}

func NewPostgresVulnerabilitySuppressionMutationStore(db *sql.DB) *PostgresVulnerabilitySuppressionMutationStore {
	return impact.NewPostgresVulnerabilitySuppressionMutationStore(db)
}

// listSupplyChainImpactReadinessQuery re-exposes the readiness SQL shape
// under its pre-move bare name for the staying root tests. The gocritic
// argOrder heuristic misfires on the qualified impact.X form inside
// strings.Contains assertions (a bare identifier of the same name passes,
// as the container-image query tests show), so the tests keep the exact
// pre-move call shape through this shim. It goes away in hub PR3 when the
// tests move into the impact package with the handlers they drive.
var listSupplyChainImpactReadinessQuery = impact.ListReadinessQuery
