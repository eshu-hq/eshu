// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id GET /api/v0/ci-cd/run-correlations serves.
const Capability = "ci_cd.run_correlations.list"

// AggregateCapability is the capability id the
// GET /api/v0/ci-cd/run-correlations/count and
// GET /api/v0/ci-cd/run-correlations/inventory routes serve.
const AggregateCapability = "ci_cd.run_correlations.aggregate"

// Support returns this family's capability contract: no local-lightweight
// support, and an exact truth ceiling on every profile that has the
// Postgres reducer read model the run correlations read. Both routes share
// the row: the list and its cheap-summary aggregates read the same
// reducer-owned facts under the same profile gate.
//
// This is the single declaration of the row. contract/cicd.go registers it
// for production and this package's main_test.go registers it for tests that
// cannot link root (#6642), so there is no second copy to drift. It returns
// fresh truth-level pointers on every call, one per ceiling, so no caller
// can mutate another's ceiling.
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
