// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func init() {
	capabilityMatrix[hostedGovernanceStatusCapability] = capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
