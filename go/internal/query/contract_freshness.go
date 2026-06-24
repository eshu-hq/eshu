// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// freshnessGenerationLifecycleCapability is the capability key for the bounded
// generation lifecycle drilldown. It reads durable scope_generations and
// fact_work_items rows from local-host Postgres and does not require the graph
// backend, so it is exact at every profile.
const freshnessGenerationLifecycleCapability = "freshness.generation_lifecycle"

func init() {
	capabilityMatrix[freshnessGenerationLifecycleCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	}
}
