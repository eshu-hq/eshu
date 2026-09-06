// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file declares the capability support row for this family's route. It
// does NOT register it: registration stays in root package query
// (contract_capability_matrix_ext.go), which owns the router and always
// links into the production binary. This package's TestMain calls this
// constructor so the family test binary exercises the same gate production
// does from a single declaration (supplychain precedent, #6060).
//
// Root's row is still a literal copy (contract_* is owned by another lane):
// a follow-up should point it at this constructor the way root's
// hardcoded-secret registration calls querycontract.HardcodedSecretSupport,
// so the two cannot drift. Until then, both spell the same contract --
// exact truth from the authoritative local profile up -- and any change to
// either must change the other in the same PR.

// OwnershipSupport is the support row for GET /api/v0/codeowners/ownership:
// exact truth from the authoritative local profile up. The route reads the
// Phase 3 DECLARES_CODEOWNER graph edges plus, for effective_owner, the
// reducer's service-catalog correlation store -- neither exists on the
// lightweight profile.
func OwnershipSupport() querycontract.CapabilitySupport {
	exact := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
