package query

import "strings"

const (
	deadCodeClassificationUnused               = "unused"
	deadCodeClassificationReachable            = "reachable"
	deadCodeClassificationExcluded             = "excluded"
	deadCodeClassificationAmbiguous            = "ambiguous"
	deadCodeClassificationDerivedCandidateOnly = "derived_candidate_only"
	deadCodeClassificationUnsupportedLanguage  = "unsupported_language"
)

func classifyDeadCodeResults(results []map[string]any, contentByID map[string]*EntityContent) {
	for _, result := range results {
		entityID := StringVal(result, "entity_id")
		result["classification"] = deadCodeResultClassification(result, contentByID[entityID])
	}
}

func deadCodeResultClassification(result map[string]any, entity *EntityContent) string {
	language := strings.ToLower(strings.TrimSpace(deadCodeEntityLanguage(result, entity)))
	if !deadCodeLanguageSupported(language) {
		return deadCodeClassificationUnsupportedLanguage
	}
	maturity := deadCodeLanguageMaturity[language]
	switch maturity {
	case deadCodeMaturityDerived:
		return deadCodeClassificationUnused
	case deadCodeMaturityDerivedCandidate:
		return deadCodeClassificationDerivedCandidateOnly
	default:
		return deadCodeClassificationAmbiguous
	}
}

func deadCodeLanguageSupported(language string) bool {
	_, ok := deadCodeLanguageMaturity[strings.ToLower(strings.TrimSpace(language))]
	return ok
}
