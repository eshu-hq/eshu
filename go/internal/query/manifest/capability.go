// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id GET
// /api/v0/repository/manifest-dependencies serves.
const Capability = "manifest.dependencies.list"

// Support returns this route's capability contract: no local-lightweight
// support, and a derived truth ceiling on every profile that runs an
// authoritative store. The rows are declared manifest facts, not resolved
// versions, so the ceiling is derived, not exact.
//
// This is the single declaration of the row. contract/capability_matrix_ext.go
// registers it for production, and this package's main_test.go registers the
// same result for tests that cannot link root, so there is no second copy to
// drift. TestCapabilityMatrixMatchesYAMLContract in root pins the assembled
// row against the YAML contract.
//
// It returns fresh truth-level pointers on every call, one per ceiling, for
// the reason workitem/capability.go gives: CapabilitySupport carries its
// ceilings as pointers, and a shared value would let one caller move another's.
func Support() querycontract.CapabilitySupport {
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
