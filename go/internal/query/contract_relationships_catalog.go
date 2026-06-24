// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// relationshipsCatalogCapability is the capability id for the typed-edge verb
// catalog and per-verb concrete edge slice that back the console Relationships
// page.
const relationshipsCatalogCapability = "platform_impact.relationships_catalog"

// init registers the relationships catalog capability in the shared capability
// matrix. It mirrors platform_impact.graph_summary_packet: unsupported on the
// lightweight profile (no authoritative graph) and exact on every profile that
// has an authoritative graph. Registration lives in this sibling file so the
// large contract.go matrix literal does not grow further.
func init() {
	capabilityMatrix[relationshipsCatalogCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
