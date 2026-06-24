// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func init() {
	for _, capability := range []string{
		semanticDocumentationObservationsCapability,
		semanticCodeHintsCapability,
	} {
		capabilityMatrix[capability] = capabilitySupport{
			LocalLightweightMax:   &truthDerived,
			LocalAuthoritativeMax: &truthDerived,
			LocalFullStackMax:     &truthDerived,
			ProductionMax:         &truthDerived,
		}
	}
}
