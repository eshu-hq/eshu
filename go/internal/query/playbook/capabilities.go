// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Support returns this family's capability contract: an exact truth ceiling
// at every profile, gated to at least the local-lightweight profile, since
// the catalog and resolver read only in-process, deterministic data and
// never touch Postgres or a graph backend.
//
// This is the declaration this package's own tests exercise. The Part C
// leaf query/contract (registry.go's CapabilityQueryPlaybooks constant and
// capability_matrix.go's baseCapabilityMatrix row) still carries its own
// equal literal for production (that leaf is off-limits to this move); a
// root test,
// TestQueryPlaybookCapabilityLockstep (capability_lockstep_query_playbook_test.go),
// asserts the two stay equal field by field until a follow-up lane adopts
// this function directly, the way contract/capability_matrix.go's
// repository.ContextOverviewCapability entry already calls
// repository.ContextOverviewSupport(). This package's own main_test.go
// registers this same function's result again for tests that cannot link
// root. See workitem/capability.go, the template this file copies.
//
// It is a function, not an exported var, and every call allocates its own
// truth levels rather than pointing at package-level ones. CapabilitySupport
// carries its ceilings as pointers, so a shared var would hand every
// caller -- including root's production registration -- write access to the
// same four ints.
//
// The four ceilings get separate variables on purpose. Returning four
// pointers to one local would leave them aliased inside the returned
// struct, so writing through any one of them would silently move the other
// three.
func Support() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelExact
	localAuthoritativeMax := querycontract.TruthLevelExact
	localFullStackMax := querycontract.TruthLevelExact
	productionMax := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalLightweight,
	}
}
