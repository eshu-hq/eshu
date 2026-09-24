// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dependency

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id GET /api/v0/dependencies serves.
const Capability = "dependencies.list"

// Support returns this route's capability contract: no local-lightweight
// support, and an exact truth ceiling on every profile that runs an
// authoritative graph.
//
// This is the single declaration of the row. contract/capability_matrix.go
// calls it for the production registration, and this package's main_test.go
// registers the same result for tests that cannot link root (#6642), so there
// is no second copy to drift. TestCapabilityMatrixMatchesYAMLContract in root
// pins the assembled row against the YAML contract.
//
// It returns fresh truth-level pointers on every call, one per ceiling, for
// the reason workitem/capability.go gives: CapabilitySupport carries its
// ceilings as pointers, and a shared value would let one caller move another's.
func Support() querycontract.CapabilitySupport {
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
