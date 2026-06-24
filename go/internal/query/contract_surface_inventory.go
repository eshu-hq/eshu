// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// surfaceInventoryCapability is the capability id for reads of the embedded
// surface inventory served at GET /api/v0/surface-inventory and the MCP
// get_surface_inventory tool. The inventory artifact is compiled in, so the
// capability is exact and supported in every profile.
const surfaceInventoryCapability = "surface_inventory.list"

func init() {
	capabilityMatrix[surfaceInventoryCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
