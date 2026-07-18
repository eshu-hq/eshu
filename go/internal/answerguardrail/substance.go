// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"strings"
	"unicode"
)

// IsCircularAnswer reports whether answer is a tautological, identity-only
// restatement of the question's entity that carries no operational fact. It is
// the deterministic substance check the runtime Ask guardrail and the offline
// answer-quality scorer share, so a generic "the payments service is a service
// named payments" answer is rejected even when citation and truth scoring pass.
//
// An answer is circular when every content token it carries also appears in the
// question, or when it carries no content token at all after identity scaffolding
// ("is", "a", "service", "workload", "named", "called", …) is removed. An answer
// that introduces even one content token absent from the question — a repository,
// a deployment, an environment, a dependency, a count — is substantive and not
// circular. An empty answer is not circular; emptiness is handled by the
// citation-coverage and boundedness rules, not this one.
func IsCircularAnswer(question, answer string) bool {
	if strings.TrimSpace(answer) == "" {
		return false
	}
	answerTokens := contentTokens(answer)
	if len(answerTokens) == 0 {
		// Only identity scaffolding and stop-words: an identity-only answer.
		return true
	}
	questionTokens := contentTokens(question)
	for tok := range answerTokens {
		if _, fromQuestion := questionTokens[tok]; !fromQuestion {
			// A content token the question did not name is a real fact.
			return false
		}
	}
	return true
}

// identityScaffolding are the words an identity-only answer is built from besides
// the entity name: copulas, articles, and the generic entity-kind and naming
// nouns a tautological answer restates. Stripping them isolates the operational
// content, if any, that an answer actually contributes.
var identityScaffolding = map[string]struct{}{
	"is": {}, "are": {}, "was": {}, "were": {}, "been": {}, "being": {},
	"the": {}, "and": {}, "for": {}, "with": {}, "this": {}, "that": {},
	"from": {}, "by": {},
	"service": {}, "services": {}, "workload": {}, "workloads": {},
	"named": {}, "called": {}, "known": {}, "referred": {}, "identified": {},
	"component": {}, "components": {}, "system": {}, "systems": {},
	"application": {}, "applications": {}, "resource": {}, "resources": {},
	"entity": {}, "entities": {}, "which": {}, "that's": {}, "its": {},
	"a": {}, "an": {}, "of": {}, "as": {}, "it": {}, "to": {}, "in": {}, "on": {},
}

// contentTokens returns the set of lowercased whole-word content tokens in text,
// excluding identity scaffolding. A token is content when it is at least three
// characters OR it carries a digit: a short numeric fact ("5", "v2", "3") is a
// real operational fact, not filler, so a terse answer like "payments exposes 5
// endpoints" must not be classed as an identity restatement. It is the shared
// tokenizer for the circular-answer heuristic.
func contentTokens(text string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		tok := strings.Trim(raw, "-_")
		if len(tok) < 3 && !hasDigit(tok) {
			continue
		}
		if _, scaffold := identityScaffolding[tok]; scaffold {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

// hasDigit reports whether tok contains at least one digit rune. A digit-bearing
// token (a count, a version, an identifier) is treated as content regardless of
// length so a terse numeric answer is never mistaken for an identity restatement.
func hasDigit(tok string) bool {
	for _, r := range tok {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
