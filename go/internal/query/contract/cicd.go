// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

func init() {
	register(cicdRunCorrelationsCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
	register(cicdRunCorrelationAggregateCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
}
