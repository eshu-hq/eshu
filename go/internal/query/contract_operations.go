// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func init() {
	capabilityMatrix[operationsStatusCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
