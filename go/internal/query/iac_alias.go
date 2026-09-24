// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved IaC/replatforming family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// iac_alias.go is the root alias shim for the IaC/replatforming handler
// family (#6642 Part A, modelled on freshness_alias.go and
// language_alias.go). Its method files moved to iac/. Names the rest of the
// program still spells `query.X` (handler.go's struct field, cmd/api's and
// cmd/mcp-server's wiring, auth_scoped_routes_iac.go, non-family staying
// files in this package, internal/mcp's replatforming parity tests, and
// staying root tests) alias here so the move touches no caller outside the
// family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import iac directly.

// IaCHandler is the IaC-quality/replatforming handler family type. Its home
// is iac/; this alias keeps handler.go's `IaC *IaCHandler` field, cmd/api's
// and cmd/mcp-server's wiring, internal/mcp's replatforming parity tests, and
// staying root tests spelling query.IaCHandler unchanged.
type IaCHandler = iac.Handler

// IaCInventoryStore reads the active-generation Postgres IaC inventory. Its
// home is iac/; the staying root test double in
// resources_scope_auth_fakes_test.go keeps spelling query.IaCInventoryStore
// unchanged.
type IaCInventoryStore = iac.InventoryStore

// IaCManagementStore reads reducer-materialized unmanaged-cloud-resource and
// replatforming-selector facts. Its home is iac/; cmd/api's and
// cmd/mcp-server's wiring keep spelling query.IaCManagementStore unchanged.
type IaCManagementStore = iac.ManagementStore

// IaCManagementFilter scopes an IaC management read. Its home is iac/;
// internal/mcp's replatforming parity fixtures keep spelling
// query.IaCManagementFilter unchanged.
type IaCManagementFilter = iac.ManagementFilter

// IaCManagementFindingRow is one unmanaged/replatforming-candidate finding
// row. Its home is iac/; internal/mcp's replatforming parity fixtures keep
// spelling query.IaCManagementFindingRow unchanged.
type IaCManagementFindingRow = iac.ManagementFindingRow

// IaCManagementEvidenceRow is one bounded evidence atom attached to an IaC
// management finding. Its home is iac/; internal/mcp's replatforming parity
// fixtures keep spelling query.IaCManagementEvidenceRow unchanged.
type IaCManagementEvidenceRow = iac.ManagementEvidenceRow

// IaCManagementSafetyGate is the bounded read-only-vs-review-required safety
// classification for an IaC management finding. Its home is iac/; staying
// non-family files (cloud_runtime_drift.go) keep spelling
// query.IaCManagementSafetyGate unchanged.
type IaCManagementSafetyGate = iac.ManagementSafetyGate

// IaCReachabilityStore reads reducer-materialized dead-IaC cleanup findings.
// Its home is iac/; the staying auth_scoped_routes_iac.go and
// dead_iac_grant_test.go keep spelling query.IaCReachabilityStore unchanged.
type IaCReachabilityStore = iac.ReachabilityStore

// IaCReachabilityFindingRow is one dead-IaC cleanup finding row. Its home is
// iac/.
type IaCReachabilityFindingRow = iac.ReachabilityFindingRow

// AWSRuntimeDriftFindingRow mirrors iac.AWSRuntimeDriftFindingRow. Its home
// is iac/; no caller outside the family spells this root name today, and it
// is kept so root's pre-move exported surface stays whole.
type AWSRuntimeDriftFindingRow = iac.AWSRuntimeDriftFindingRow

// IaCInventoryCandidate, IaCInventorySearch, and IaCInventorySummary mirror
// iac's InventoryCandidate/InventorySearch/InventorySummary -- the
// IaCInventoryStore interface's own parameter/return types, exported from
// the leaf (#6642 Part A) so any adapter, including a root test double, can
// implement the interface at all. resources_scope_auth_fakes_test.go's
// duplicated stubIaCInventoryStore keeps spelling these root names.
type IaCInventoryCandidate = iac.InventoryCandidate

// IaCInventorySearch mirrors iac.InventorySearch. See IaCInventoryCandidate.
type IaCInventorySearch = iac.InventorySearch

// IaCInventorySummary mirrors iac.InventorySummary. See IaCInventoryCandidate.
type IaCInventorySummary = iac.InventorySummary

// PostgresIaCInventoryStore adapts Postgres to IaCInventoryStore. Its home is
// iac/; cmd/api's and cmd/mcp-server's wiring construct it through
// NewPostgresIaCInventoryStore below.
type PostgresIaCInventoryStore = iac.PostgresIaCInventoryStore

// NewPostgresIaCInventoryStore constructs the Postgres-backed
// IaCInventoryStore adapter. Its home is iac/; cmd/api's and cmd/mcp-server's
// wiring keep spelling query.NewPostgresIaCInventoryStore unchanged. db
// satisfies iac's unexported inventoryQueryer interface structurally, so this
// forwarder can spell the concrete *sql.DB type without importing that
// unexported interface.
func NewPostgresIaCInventoryStore(db *sql.DB) PostgresIaCInventoryStore {
	return iac.NewPostgresIaCInventoryStore(db)
}

// PostgresIaCManagementStore adapts Postgres to IaCManagementStore. Its home
// is iac/; cmd/api's and cmd/mcp-server's wiring construct it through
// NewPostgresIaCManagementStore below.
type PostgresIaCManagementStore = iac.PostgresIaCManagementStore

// NewPostgresIaCManagementStore constructs the Postgres-backed
// IaCManagementStore adapter. Its home is iac/; cmd/api's and
// cmd/mcp-server's wiring keep spelling query.NewPostgresIaCManagementStore
// unchanged.
func NewPostgresIaCManagementStore(db *sql.DB) *PostgresIaCManagementStore {
	return iac.NewPostgresIaCManagementStore(db)
}

// PostgresIaCReachabilityStore adapts Postgres to IaCReachabilityStore. Its
// home is iac/; cmd/api's and cmd/mcp-server's wiring construct it through
// NewPostgresIaCReachabilityStore below.
type PostgresIaCReachabilityStore = iac.PostgresIaCReachabilityStore

// NewPostgresIaCReachabilityStore constructs the Postgres-backed
// IaCReachabilityStore adapter. Its home is iac/; cmd/api's and
// cmd/mcp-server's wiring keep spelling query.NewPostgresIaCReachabilityStore
// unchanged.
func NewPostgresIaCReachabilityStore(db *sql.DB) *PostgresIaCReachabilityStore {
	return iac.NewPostgresIaCReachabilityStore(db)
}

// ReplatformingSourceState is the closed source-state taxonomy shared by the
// replatforming plan/rollup surfaces and the staying cloud-inventory read
// model. Its home is iac/; cloud_inventory_read_model.go and
// cloud_runtime_drift_view.go keep spelling query.ReplatformingSourceState
// unchanged.
type ReplatformingSourceState = iac.ReplatformingSourceState

// Replatforming source-state enumeration values. cloud_inventory_read_model.go
// uses Exact, Derived, and Unknown; cloud_runtime_drift_test.go and
// cloud_inventory_readback_test.go use Derived, Exact, and Rejected. The
// remaining values are caller-free today (outside the family) and stay so
// this alias stanza keeps the whole enumeration, not a subset a future
// caller would find half-missing (see freshness_alias.go's identical
// rationale for FreshnessCause).
const (
	ReplatformingSourceStateAmbiguous   = iac.ReplatformingSourceStateAmbiguous
	ReplatformingSourceStateDerived     = iac.ReplatformingSourceStateDerived
	ReplatformingSourceStateExact       = iac.ReplatformingSourceStateExact
	ReplatformingSourceStatePartial     = iac.ReplatformingSourceStatePartial
	ReplatformingSourceStateRejected    = iac.ReplatformingSourceStateRejected
	ReplatformingSourceStateStale       = iac.ReplatformingSourceStateStale
	ReplatformingSourceStateUnavailable = iac.ReplatformingSourceStateUnavailable
	ReplatformingSourceStateUnknown     = iac.ReplatformingSourceStateUnknown
	ReplatformingSourceStateUnsupported = iac.ReplatformingSourceStateUnsupported
)

// AllReplatformingSourceStates lists every closed source-state value in
// stable order. Its home is iac/; internal/mcp's replatforming parity test
// keeps spelling query.AllReplatformingSourceStates unchanged.
func AllReplatformingSourceStates() []ReplatformingSourceState {
	return iac.AllReplatformingSourceStates()
}

// ResolveReplatformingSourceState derives the closed source state from a
// management status and promotion-rejected flag. Its home is iac/;
// cloud_runtime_drift_view.go keeps spelling
// query.ResolveReplatformingSourceState unchanged.
func ResolveReplatformingSourceState(managementStatus string, promotionRejected bool) ReplatformingSourceState {
	return iac.ResolveReplatformingSourceState(managementStatus, promotionRejected)
}

// ReplatformingSelectorPage is one bounded page of replatforming selector
// scopes. Its home is iac/; the staying replatforming_selectors_handler_test.go
// keeps spelling query.ReplatformingSelectorPage unchanged.
type ReplatformingSelectorPage = iac.ReplatformingSelectorPage

// ReplatformingSelectorScope is one AWS replatforming-review selector scope.
// Its home is iac/; the staying replatforming_selectors_handler_test.go keeps
// spelling query.ReplatformingSelectorScope unchanged.
type ReplatformingSelectorScope = iac.ReplatformingSelectorScope

// ReplatformingSelectorStore reads the active AWS replatforming selector
// inventory. Its home is iac/.
type ReplatformingSelectorStore = iac.ReplatformingSelectorStore

// replatformingSelectorLabel mirrors iac.ReplatformingSelectorLabel. Its home
// is iac/; the staying replatforming_selectors_handler_test.go keeps
// spelling query.replatformingSelectorLabel unchanged.
func replatformingSelectorLabel(scope ReplatformingSelectorScope) string {
	return iac.ReplatformingSelectorLabel(scope)
}

// ReplatformingPlan is the migration-readiness plan contract. Its home is
// iac/; the staying replatforming_plan_contract_test.go keeps spelling
// query.ReplatformingPlan unchanged.
type ReplatformingPlan = iac.ReplatformingPlan

// NewReplatformingPlan mirrors iac.NewReplatformingPlan.
func NewReplatformingPlan(scope ReplatformingPlanScope) ReplatformingPlan {
	return iac.NewReplatformingPlan(scope)
}

// ReplatformingPlanScope is the plan scope. Its home is iac/.
type ReplatformingPlanScope = iac.ReplatformingPlanScope

// ReplatformingScopeKind is the closed plan-scope kind taxonomy. Its home is
// iac/.
type ReplatformingScopeKind = iac.ReplatformingScopeKind

// MigrationPacketItem is one plan item. Its home is iac/; the staying
// replatforming_plan_contract_test.go keeps spelling
// query.MigrationPacketItem unchanged.
type MigrationPacketItem = iac.MigrationPacketItem

// MigrationWave is one plan blast-radius wave. Its home is iac/.
type MigrationWave = iac.MigrationWave

// BlastRadiusGroup is the closed blast-radius severity grouping. Its home is
// iac/.
type BlastRadiusGroup = iac.BlastRadiusGroup

// ReplatformingImportCandidate is one item's Terraform import readiness. Its
// home is iac/; the staying replatforming_plan_contract_test.go keeps
// spelling query.ReplatformingImportCandidate unchanged.
type ReplatformingImportCandidate = iac.ReplatformingImportCandidate

// ReplatformingOwnerCandidate is one item's ownership candidate. Its home is
// iac/; the staying replatforming_plan_contract_test.go keeps spelling
// query.ReplatformingOwnerCandidate unchanged.
type ReplatformingOwnerCandidate = iac.ReplatformingOwnerCandidate

// ReplatformingSourceLayer is one source-evidence layer feeding a plan item.
// Its home is iac/.
type ReplatformingSourceLayer = iac.ReplatformingSourceLayer

// ReplatformingSourceLayerStatus is the closed source-layer status
// taxonomy. Its home is iac/.
type ReplatformingSourceLayerStatus = iac.ReplatformingSourceLayerStatus

// Replatforming plan/scope/source-layer enumeration values. The staying
// replatforming_plan_contract_test.go uses ReplatformingImportStatusReady,
// ReplatformingImportStatusRefused, ReplatformingScopeAccount, and
// ReplatformingScopeService. The remaining values are caller-free today
// (outside the family) and stay so this stanza keeps the whole enumeration.
const (
	ReplatformingPlanContractVersion   = iac.ReplatformingPlanContractVersion
	ReplatformingImportStatusReady     = iac.ReplatformingImportStatusReady
	ReplatformingImportStatusRefused   = iac.ReplatformingImportStatusRefused
	ReplatformingScopeAccount          = iac.ReplatformingScopeAccount
	ReplatformingScopeEnvironment      = iac.ReplatformingScopeEnvironment
	ReplatformingScopeRegion           = iac.ReplatformingScopeRegion
	ReplatformingScopeRepository       = iac.ReplatformingScopeRepository
	ReplatformingScopeResource         = iac.ReplatformingScopeResource
	ReplatformingScopeService          = iac.ReplatformingScopeService
	ReplatformingScopeWorkload         = iac.ReplatformingScopeWorkload
	ReplatformingSourceAppliedState    = iac.ReplatformingSourceAppliedState
	ReplatformingSourceDeclaredIaC     = iac.ReplatformingSourceDeclaredIaC
	ReplatformingSourceMissingEvidence = iac.ReplatformingSourceMissingEvidence
	ReplatformingSourceObservedRuntime = iac.ReplatformingSourceObservedRuntime
)

// managementStatus* mirror the eight closed IaC management status values
// this family declares (iac.ManagementStatus*). Their home is iac/; five
// staying root tests (replatforming_plan_waves_handler_test.go,
// replatforming_plan_contract_test.go, replatforming_ownership_handler_test.go,
// replatforming_plan_handler_test.go, replatforming_source_state_test.go)
// keep spelling the lowercase root names unchanged.
const (
	managementStatusAmbiguous           = iac.ManagementStatusAmbiguous
	managementStatusCloudOnly           = iac.ManagementStatusCloudOnly
	managementStatusManagedByOtherIaC   = iac.ManagementStatusManagedByOtherIaC
	managementStatusManagedByTerraform  = iac.ManagementStatusManagedByTerraform
	managementStatusStaleIaCCandidate   = iac.ManagementStatusStaleIaCCandidate
	managementStatusTerraformConfigOnly = iac.ManagementStatusTerraformConfigOnly
	managementStatusTerraformStateOnly  = iac.ManagementStatusTerraformStateOnly
	managementStatusUnknown             = iac.ManagementStatusUnknown
)

// replatforming{BlastGroup,Wave}* and replatformingRollup*Key mirror the
// closed blast-radius grouping, wave, and rollup-bucket-key values this
// family declares (iac.Replatforming*). Their home is iac/; staying root
// tests (replatforming_plan_waves_handler_test.go,
// replatforming_rollups_handler_test.go) keep spelling the lowercase root
// names unchanged. The full blast-group enumeration stays aliased even
// though only three values have a current staying caller.
const (
	replatformingBlastGroupBlocked     = iac.ReplatformingBlastGroupBlocked
	replatformingBlastGroupHigh        = iac.ReplatformingBlastGroupHigh
	replatformingBlastGroupLow         = iac.ReplatformingBlastGroupLow
	replatformingBlastGroupMedium      = iac.ReplatformingBlastGroupMedium
	replatformingBlastGroupNone        = iac.ReplatformingBlastGroupNone
	replatformingWaveBlocked           = iac.ReplatformingWaveBlocked
	replatformingWaveEarly             = iac.ReplatformingWaveEarly
	replatformingWaveReview            = iac.ReplatformingWaveReview
	replatformingRollupAmbiguousKey    = iac.ReplatformingRollupAmbiguousKey
	replatformingRollupUnattributedKey = iac.ReplatformingRollupUnattributedKey
)

// findingKind* mirror the six closed IaC/replatforming finding-kind values
// this family declares (iac.FindingKind*). Their home is iac/; staying root
// tests (replatforming_ownership_handler_test.go, replatforming_plan_handler_test.go,
// replatforming_plan_waves_handler_test.go, replatforming_rollups_handler_test.go,
// resources_scope_auth_test.go and others) keep spelling the lowercase root
// names unchanged. All six stay aliased, not just the four with a current
// staying caller, so this stanza keeps the whole enumeration.
const (
	findingKindAmbiguousCloudResource      = iac.FindingKindAmbiguousCloudResource
	findingKindImageVersionDrift           = iac.FindingKindImageVersionDrift
	findingKindOrphanedCloudResource       = iac.FindingKindOrphanedCloudResource
	findingKindUnknownCloudResource        = iac.FindingKindUnknownCloudResource
	findingKindUnmanagedCloudResource      = iac.FindingKindUnmanagedCloudResource
	findingKindValueComparisonInconclusive = iac.FindingKindValueComparisonInconclusive
)

// ReplatformingSourceStateForManagementStatus mirrors the identically named
// iac function. Its home is iac/; the staying replatforming_source_state_test.go
// keeps spelling query.ReplatformingSourceStateForManagementStatus unchanged.
func ReplatformingSourceStateForManagementStatus(status string) ReplatformingSourceState {
	return iac.ReplatformingSourceStateForManagementStatus(status)
}

// ReplatformingSourceStateForMultiCloudQueryState mirrors the identically
// named iac function. Its home is iac/; the staying
// replatforming_source_state_test.go keeps spelling
// query.ReplatformingSourceStateForMultiCloudQueryState unchanged.
func ReplatformingSourceStateForMultiCloudQueryState(state string) ReplatformingSourceState {
	return iac.ReplatformingSourceStateForMultiCloudQueryState(state)
}

// replatformingOwnershipCapability mirrors iac.ReplatformingOwnershipCapability.
// Its home is iac/; query/contract/replatforming_ownership.go registers the
// matrix row from its own literal, and capability_lockstep_iac_test.go asserts
// this root spelling stays byte-identical to it, so this forwarding const must
// keep the identical string value.
const replatformingOwnershipCapability = iac.ReplatformingOwnershipCapability

// replatformingRollupsCapability mirrors iac.ReplatformingRollupsCapability.
// Its home is iac/; query/contract/replatforming_rollups.go registers the row
// from its own literal, and capability_lockstep_iac_test.go asserts this root
// spelling stays byte-identical to it, so the value must not drift.
const replatformingRollupsCapability = iac.ReplatformingRollupsCapability

// replatformingPlanRoute mirrors iac.ReplatformingPlanRoute. Its home is
// iac/; auth_scoped_routes_iac.go keeps spelling the root name unchanged.
const replatformingPlanRoute = iac.ReplatformingPlanRoute

// iacManagementSafetyGate mirrors iac.NewManagementSafetyGate. Its home is
// iac/; cloud_runtime_drift_view.go keeps spelling
// query.iacManagementSafetyGate unchanged.
func iacManagementSafetyGate(status string, warnings []string, redactions []string) IaCManagementSafetyGate {
	return iac.NewManagementSafetyGate(status, warnings, redactions)
}

// normalizeIaCManagementFindingKinds mirrors iac.NormalizeManagementFindingKinds.
// Its home is iac/; cloud_runtime_drift.go keeps spelling
// query.normalizeIaCManagementFindingKinds unchanged.
func normalizeIaCManagementFindingKinds(raw []string) ([]string, error) {
	return iac.NormalizeManagementFindingKinds(raw)
}

// awsCloudRuntimeDriftDerivedStatus mirrors iac.AWSCloudRuntimeDriftDerivedStatus.
// Its home is iac/; cloud_runtime_drift_aggregate.go keeps spelling
// query.awsCloudRuntimeDriftDerivedStatus unchanged.
func awsCloudRuntimeDriftDerivedStatus(
	row postgres.AWSCloudRuntimeDriftFindingRow,
) (status string, missingEvidence []string, warningFlags []string) {
	return iac.AWSCloudRuntimeDriftDerivedStatus(row)
}

// driftedAttributesFromAWSEvidence mirrors iac.DriftedAttributesFromAWSEvidence.
// Its home is iac/; cloud_runtime_drift_aggregate.go keeps spelling
// query.driftedAttributesFromAWSEvidence unchanged.
func driftedAttributesFromAWSEvidence(evidence []postgres.AWSCloudRuntimeDriftEvidenceRow) []querycontract.DriftedAttributeView {
	return iac.DriftedAttributesFromAWSEvidence(evidence)
}

// awsRuntimeDriftRowToIaCManagement mirrors iac.AWSRuntimeDriftRowToManagement.
// Its home is iac/; the staying test cloud_runtime_drift_aggregate_test.go
// keeps spelling query.awsRuntimeDriftRowToIaCManagement unchanged.
func awsRuntimeDriftRowToIaCManagement(row postgres.AWSCloudRuntimeDriftFindingRow) IaCManagementFindingRow {
	return iac.AWSRuntimeDriftRowToManagement(row)
}

// normalizeIaCManagementFindingSafety mirrors iac.NormalizeManagementFindingSafety.
// Its home is iac/; resources_scope_auth_fakes_test.go-style staying test
// fixtures (replatforming_ownership_handler_test.go's own ownershipFinding
// duplicate) keep spelling the root name unchanged.
func normalizeIaCManagementFindingSafety(finding *IaCManagementFindingRow) {
	iac.NormalizeManagementFindingSafety(finding)
}

// ReplatformingPlanNonGoals mirrors iac.ReplatformingPlanNonGoals. Its home
// is iac/; no caller outside the family spells this root name today, and it
// is kept so root's pre-move exported surface stays whole.
func ReplatformingPlanNonGoals() []string {
	return iac.ReplatformingPlanNonGoals()
}
