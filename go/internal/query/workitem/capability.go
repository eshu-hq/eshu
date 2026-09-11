// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// EvidenceSupport returns this family's work-item-evidence capability
// contract: an exact truth ceiling at every profile, since the route serves
// active work-item source facts directly with no derived or fallback
// truth level.
//
// This is the declaration this package's own tests exercise. Root package
// query's contract_work_item.go still carries its own equal
// capabilitySupport literal for production (#6642 Part A leaves that file to
// Part C to update), rather than calling this function the way
// repository/capability.go's template is called from root; a root test,
// TestWorkItemEvidenceCapabilityLockstep
// (capability_lockstep_work_item_test.go), asserts the two stay equal field
// by field until Part C adopts this function directly. This package's own
// main_test.go registers this same function's result again for tests that
// cannot link root (#6642). See repository/capability.go, the template this
// file copies.
//
// It is a function, not an exported var, and every call allocates its own
// truth levels rather than pointing at package-level ones. CapabilitySupport
// carries its ceilings as pointers, so a shared var would hand every caller —
// including root's production registration — write access to the same four
// ints.
//
// The four ceilings get separate variables on purpose. Returning four
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other three.
func EvidenceSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelExact
	localAuthoritativeMax := querycontract.TruthLevelExact
	localFullStackMax := querycontract.TruthLevelExact
	productionMax := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
}
