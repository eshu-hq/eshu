// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func init() {
	capabilityMatrix["call_graph.metrics"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
