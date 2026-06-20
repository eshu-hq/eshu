package query

const askNaturalLanguageAnswerCapability = "ask.natural_language_answer"

func init() {
	capabilityMatrix[askNaturalLanguageAnswerCapability] = capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
