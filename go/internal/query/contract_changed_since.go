// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// freshnessChangedSinceCapability is the capability key for the bounded
// changed-since delta summary. It diffs a prior generation's fact set against
// the current active generation's fact set in local-host Postgres
// (fact_records joined with ingestion_scopes and scope_generations) and does not
// require the graph backend, so it is exact at every profile.
const freshnessChangedSinceCapability = "freshness.changed_since"

func init() {
	capabilityMatrix[freshnessChangedSinceCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	}
}
