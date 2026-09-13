// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

func init() {
	for _, capability := range []string{
		semanticDocumentationObservationsCapability,
		semanticCodeHintsCapability,
	} {
		register(capability, capabilitySupport{
			LocalLightweightMax:   &truthDerived,
			LocalAuthoritativeMax: &truthDerived,
			LocalFullStackMax:     &truthDerived,
			ProductionMax:         &truthDerived,
		})
	}
}
