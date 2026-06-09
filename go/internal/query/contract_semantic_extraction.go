package query

func init() {
	capabilityMatrix[semanticExtractionStatusCapability] = capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	}
}
