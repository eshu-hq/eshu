// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	if deadCodeResultHasExactnessBlockers(result, entity) {
		return deadCodeClassificationAmbiguous
	}
	if deadCodeResultHasWeakIncomingEdge(result) {
		return deadCodeClassificationAmbiguous
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

func deadCodeResultHasExactnessBlockers(result map[string]any, entity *EntityContent) bool {
	if metadata, ok := result["metadata"].(map[string]any); ok {
		if len(StringSliceVal(metadata, "exactness_blockers")) > 0 {
			return true
		}
	}
	return entity != nil && len(StringSliceVal(entity.Metadata, "exactness_blockers")) > 0
}

// deadCodeResultHasWeakIncomingEdge reports whether the incoming-edge probe
// stamped this result as reachable only by the weakest (repo_unique_name)
// resolution tier, which makes the candidate ambiguous rather than confidently
// unused.
func deadCodeResultHasWeakIncomingEdge(result map[string]any) bool {
	weak, _ := result[deadCodeWeakIncomingResultKey].(bool)
	return weak
}

func deadCodeLanguageSupported(language string) bool {
	_, ok := deadCodeLanguageMaturity[strings.ToLower(strings.TrimSpace(language))]
	return ok
}
