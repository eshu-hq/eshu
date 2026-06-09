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
