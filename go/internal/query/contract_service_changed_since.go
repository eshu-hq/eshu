// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// freshnessServiceChangedSinceCapability is the capability key for the bounded
// service-scope changed-since delta summary (#1943). It diffs a prior service
// materialization generation's evidence snapshot set against the current active
// generation's set in local-host Postgres (service_evidence_snapshots joined with
// service_materialization_generations) and does not require the graph backend, so
// it is exact at every profile. It reports the ownership (#1943), deployment
// (#1985), runtime (#1986), and dependencies (#1987) evidence families.
const freshnessServiceChangedSinceCapability = "freshness.service_changed_since"

func init() {
	capabilityMatrix[freshnessServiceChangedSinceCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	}
}
