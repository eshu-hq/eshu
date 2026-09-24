// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id GET /api/v0/kubernetes/correlations serves.
const Capability = "kubernetes.correlations.list"

// Support returns this route's capability contract: no local-lightweight
// support, and an exact truth ceiling on every profile that runs the Postgres
// reducer read model.
//
// This is the single declaration of the row. contract/kubernetes.go
// registers it for production and this package's main_test.go registers it for
// tests that cannot link root (#6642), so there is no second copy to drift.
// It returns fresh truth-level pointers on every call, one per ceiling, for
// the reason workitem/capability.go gives.
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
