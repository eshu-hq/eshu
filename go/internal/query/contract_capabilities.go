// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// capabilityCatalogCapability is the capability id for reads of the embedded
// capability catalog served at GET /api/v0/capabilities and the MCP
// get_capability_catalog tool. The catalog artifact is compiled in, so the
// capability is exact and supported in every profile.
const capabilityCatalogCapability = "capability_catalog.list"

func init() {
	capabilityMatrix[capabilityCatalogCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
