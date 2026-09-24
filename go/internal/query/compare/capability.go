// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id POST /api/v0/compare/environments serves.
const Capability = "platform_impact.environment_compare"

// Support returns this route's capability contract: no local-lightweight
// support, and an exact truth ceiling on every profile that has the
// canonical graph the comparison reads.
//
// This is the single declaration of the row. contract/capability_matrix.go
// registers it for production and this package's main_test.go registers it for
// tests that cannot link root (#6642), so there is no second copy to drift.
// It returns fresh truth-level pointers on every call, one per ceiling, so no
// caller can mutate another's ceiling.
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
