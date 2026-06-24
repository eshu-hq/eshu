// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
