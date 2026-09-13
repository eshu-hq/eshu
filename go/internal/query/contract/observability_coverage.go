// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

func init() {
	register(observabilityCoverageCorrelationsCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
}
