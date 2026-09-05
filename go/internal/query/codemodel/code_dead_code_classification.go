// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeadCodeHiddenConsumerResultKey marks a kept candidate whose only incoming
// edges came from repositories outside the caller's grant, and
// DeadCodeHiddenConsumerReason is the investigation reason it reports. The
// value is the same permission_hidden_consumer the cross-repo route already
// answers with, so one vocabulary covers both dead-code shapes.
// The hidden-consumer markers below are exported because the staying scan
// stamps through the result key and the staying investigation reports
// through the reason, both via the root forwards.
const (
	DeadCodeHiddenConsumerResultKey = "permission_hidden_consumer"
	DeadCodeHiddenConsumerReason    = "permission_hidden_consumer"
)

// The weak-incoming marker keys below are family-local copies of root's
// code_dead_code_scan.go constants of the same names. The ambiguous
// classifier reads them, and the staying scan that shares them cannot cross
// the package boundary, so the leaf carries these byte-identical copies
// instead of importing root. Keep them behavior-identical to root.
const (
	deadCodeWeakIncomingResultKey   = "weak_incoming_only"
	deadCodeWeakIncomingMethodKey   = "weak_incoming_method"
	deadCodeWeakIncomingReasonScope = "weak_incoming_edge:"
)

// The classification vocabulary below is partly exported: Unused,
// Excluded, and Ambiguous are compared by the staying scan, investigation,
// and cross-repo readers via the root forwards; the rest serve only this
// package.
const (
	DeadCodeClassificationUnused               = "unused"
	deadCodeClassificationReachable            = "reachable"
	DeadCodeClassificationExcluded             = "excluded"
	DeadCodeClassificationAmbiguous            = "ambiguous"
	deadCodeClassificationDerivedCandidateOnly = "derived_candidate_only"
	deadCodeClassificationUnsupportedLanguage  = "unsupported_language"
)

// ClassifyDeadCodeResults stamps every scan result with its dead-code classification.
func ClassifyDeadCodeResults(results []map[string]any, contentByID map[string]*querycontract.EntityContent) {
	for _, result := range results {
		entityID := querycontract.StringVal(result, "entity_id")
		result["classification"] = DeadCodeResultClassification(result, contentByID[entityID])
	}
}

// DeadCodeResultClassification returns the dead-code classification for one result.
func DeadCodeResultClassification(result map[string]any, entity *querycontract.EntityContent) string {
	language := strings.ToLower(strings.TrimSpace(deadCodeEntityLanguage(result, entity)))
	if !DeadCodeLanguageSupported(language) {
		return deadCodeClassificationUnsupportedLanguage
	}
	if deadCodeResultHasExactnessBlockers(result, entity) {
		return DeadCodeClassificationAmbiguous
	}
	if deadCodeResultHasWeakIncomingEdge(result) || DeadCodeResultHasHiddenConsumer(result) {
		return DeadCodeClassificationAmbiguous
	}
	maturity := DeadCodeLanguageMaturity[language]
	switch maturity {
	case deadCodeMaturityDerived:
		return DeadCodeClassificationUnused
	case deadCodeMaturityDerivedCandidate:
		return deadCodeClassificationDerivedCandidateOnly
	default:
		return DeadCodeClassificationAmbiguous
	}
}

func deadCodeResultHasExactnessBlockers(result map[string]any, entity *querycontract.EntityContent) bool {
	if metadata, ok := result["metadata"].(map[string]any); ok {
		if len(querycontract.StringSliceVal(metadata, "exactness_blockers")) > 0 {
			return true
		}
	}
	return entity != nil && len(querycontract.StringSliceVal(entity.Metadata, "exactness_blockers")) > 0
}

// deadCodeResultHasWeakIncomingEdge reports whether the incoming-edge probe
// stamped this result as reachable only by the weakest (repo_unique_name)
// resolution tier, which makes the candidate ambiguous rather than confidently
// unused.
func deadCodeResultHasWeakIncomingEdge(result map[string]any) bool {
	weak, _ := result[deadCodeWeakIncomingResultKey].(bool)
	return weak
}

// DeadCodeLanguageSupported reports whether dead-code analysis models the language.
func DeadCodeLanguageSupported(language string) bool {
	_, ok := DeadCodeLanguageMaturity[strings.ToLower(strings.TrimSpace(language))]
	return ok
}

// DeadCodeResultHasHiddenConsumer reports whether the incoming-edge probe found
// a consumer in a repository the caller was not granted, which makes the
// candidate unknown rather than either reachable or unused.
func DeadCodeResultHasHiddenConsumer(result map[string]any) bool {
	hidden, _ := result[DeadCodeHiddenConsumerResultKey].(bool)
	return hidden
}

// DeadCodeWeakIncomingAmbiguityReason returns the investigation reason string
// for a candidate that is ambiguous because its only incoming edges were weak
// (repo_unique_name tier), naming the resolution method that triggered it.
func DeadCodeWeakIncomingAmbiguityReason(result map[string]any) (string, bool) {
	if !deadCodeResultHasWeakIncomingEdge(result) {
		return "", false
	}
	method := strings.TrimSpace(querycontract.StringVal(result, deadCodeWeakIncomingMethodKey))
	if method == "" {
		method = codeprovenance.MethodRepoUniqueName
	}
	return deadCodeWeakIncomingReasonScope + method, true
}
