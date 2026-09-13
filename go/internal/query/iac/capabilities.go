// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file declares the capability support rows for this family's nine
// owned routes. It does NOT register them: registration stays in root
// package query (contract_capability_matrix.go), which owns the router and
// always links into the production binary. This package's TestMain calls
// these constructors so the family test binary exercises the same gate
// production does from a single declaration (freshness/supplychain/codeowners
// precedent, #6060).
//
// Root's rows are still literal capabilityMatrix map entries today
// (contract_capability_matrix.go is owned by another #6642 lane, Part C): a
// follow-up should point them at these constructors the way the repository
// family's row already does (repository.ContextOverviewCapability:
// repository.ContextOverviewSupport() in contract_capability_matrix.go), so
// the two cannot drift. Until then, both spell the same contract, and any
// change to either must change the other in the same PR.
// capability_lockstep_iac_test.go (root, package query) pins this equality
// field by field for every capability below.
//
// ReplatformingPlanReadinessCapability and ReplatformingSelectorInventoryCapability
// mirror two capability IDs this family's routes gate but does not own:
// root's contract_replatforming.go (Part C, not editable by this move)
// declares and registers replatformingPlanReadinessCapability and
// replatformingSelectorInventoryCapability as unexported root constants. A
// leaf package cannot see an unexported root constant, so this family
// declares its own exported constant carrying the identical string value.
// capability_lockstep_iac_test.go also pins these two spellings equal; there
// is no Support() constructor for either because this family does not own
// their registration.

// The nine capability ID constants this family owns (DeadCapability,
// ManagementCapability, ManagementStatusCapability, ManagementExplainCapability,
// TerraformImportCapability, AWSRuntimeDriftFindingsCapability,
// ResourcesCapability, ReplatformingOwnershipCapability,
// ReplatformingRollupsCapability) stay declared next to the routes that use
// them (handler.go, resources.go, aws_runtime_drift.go,
// replatforming_ownership_handler.go, replatforming_rollups_handler.go) --
// the same layout root used before the move. Only the Support() constructors
// for those nine, plus the two Part C mirror constants below, live here.

// ReplatformingPlanReadinessCapability mirrors root's unexported
// replatformingPlanReadinessCapability (contract_replatforming.go, Part C).
// See the file doc comment above for why this family carries its own copy
// of the string instead of the root constant.
const ReplatformingPlanReadinessCapability = "replatforming.plan.readiness"

// ReplatformingSelectorInventoryCapability mirrors root's unexported
// replatformingSelectorInventoryCapability (contract_replatforming.go, Part
// C). See the file doc comment above for why this family carries its own
// copy of the string instead of the root constant.
const ReplatformingSelectorInventoryCapability = "replatforming.selector_inventory"

// iacManagementDerivedSupport returns the shared capability contract most of
// this family's routes carry: derived truth at both local-authoritative
// profiles and production, unsupported at local-lightweight because that
// profile cannot materialize the reducer-owned evidence these routes read.
// The four ceilings get separate variables on purpose. Returning four
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other three.
func iacManagementDerivedSupport() querycontract.CapabilitySupport {
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}

// DeadSupport returns DeadCapability's capability contract. See
// iacManagementDerivedSupport for the shared shape and why the four ceilings
// are not one shared pointer.
func DeadSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ManagementSupport returns ManagementCapability's capability contract. See
// iacManagementDerivedSupport for the shared shape.
func ManagementSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ManagementStatusSupport returns ManagementStatusCapability's capability
// contract. See iacManagementDerivedSupport for the shared shape.
func ManagementStatusSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ManagementExplainSupport returns ManagementExplainCapability's capability
// contract. See iacManagementDerivedSupport for the shared shape.
func ManagementExplainSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// TerraformImportSupport returns TerraformImportCapability's capability
// contract. See iacManagementDerivedSupport for the shared shape.
func TerraformImportSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// AWSRuntimeDriftFindingsSupport returns AWSRuntimeDriftFindingsCapability's
// capability contract. See iacManagementDerivedSupport for the shared shape.
func AWSRuntimeDriftFindingsSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ReplatformingOwnershipSupport returns ReplatformingOwnershipCapability's
// capability contract. See iacManagementDerivedSupport for the shared shape.
func ReplatformingOwnershipSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ReplatformingRollupsSupport returns ReplatformingRollupsCapability's
// capability contract. See iacManagementDerivedSupport for the shared shape.
func ReplatformingRollupsSupport() querycontract.CapabilitySupport {
	return iacManagementDerivedSupport()
}

// ResourcesSupport returns ResourcesCapability's capability contract: exact
// truth at every local-authoritative-or-above profile because the resource
// browse route hydrates from an authoritative graph label with a
// generation-exact fail-closed check, never a derived projection.
func ResourcesSupport() querycontract.CapabilitySupport {
	localAuthoritativeMax := querycontract.TruthLevelExact
	localFullStackMax := querycontract.TruthLevelExact
	productionMax := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
