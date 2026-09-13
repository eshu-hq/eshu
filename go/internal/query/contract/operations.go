// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

func init() {
	register(operationsStatusCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
}
