// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id GET /api/v0/metrics/timeseries serves.
const Capability = "platform_metrics.timeseries"

// Support returns this route's capability contract: a derived truth ceiling
// on every profile, including local-lightweight, since the series come from
// the configured Prometheus-compatible source rather than from the reducer.
//
// This is the single declaration of the row. contract/metrics.go registers it
// for production and this package's main_test.go registers it for tests that
// cannot link root (#6642), so there is no second copy to drift. It returns
// fresh truth-level pointers on every call, one per ceiling, so no caller can
// mutate another's ceiling.
func Support() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelDerived
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
}
