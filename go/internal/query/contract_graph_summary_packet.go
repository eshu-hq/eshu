// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// graphSummaryPacketCapability is the capability id for the bounded graph
// summary packet (hot entities, key relationships, ecosystem map).
const graphSummaryPacketCapability = "platform_impact.graph_summary_packet"

// init registers the graph summary packet capability in the shared capability
// matrix. It mirrors platform_impact.context_overview: unsupported on the
// lightweight profile (no authoritative graph) and exact on every profile that
// has an authoritative graph. Registration lives in this sibling file so the
// large contract.go matrix literal does not grow further.
func init() {
	capabilityMatrix[graphSummaryPacketCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
