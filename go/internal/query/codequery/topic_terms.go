// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"regexp"
	"slices"
	"strings"
)

// Search-term derivation for code-topic investigation: tokenizes the topic
// and intent, expands synonyms, and drops stop words and short tokens.
// Split out of topic.go for the 500-line cap.

var codeTopicWordPattern = regexp.MustCompile(`[a-z0-9]+`)

func CodeTopicSearchTerms(topic, intent string, explicit []string) []string {
	terms := make([]string, 0, codeTopicMaxTerms)
	seen := map[string]struct{}{}
	add := func(term string) {
		term = normalizeCodeTopicTerm(term)
		if term == "" {
			return
		}
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	for _, term := range explicit {
		add(term)
	}
	for _, token := range codeTopicWordPattern.FindAllString(strings.ToLower(topic+" "+intent), -1) {
		add(token)
		for _, synonym := range codeTopicSynonyms(token) {
			add(synonym)
		}
		if len(terms) >= codeTopicMaxTerms {
			break
		}
	}
	if len(terms) > codeTopicMaxTerms {
		return terms[:codeTopicMaxTerms]
	}
	slices.SortStableFunc(terms, func(a, b string) int {
		return strings.Compare(a, b)
	})
	return terms
}

func normalizeCodeTopicTerm(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))
	switch {
	case term == "":
		return ""
	case codeTopicStopWords[term]:
		return ""
	case len(term) < 3:
		return ""
	default:
		return term
	}
}

func codeTopicSynonyms(token string) []string {
	switch token {
	case "authentication", "authenticate", "authenticated", "credentials", "credential":
		return []string{"auth", "token"}
	case "github":
		return []string{"installation", "app"}
	case "locking", "locked", "locks":
		return []string{"lock"}
	case "repositories", "repository":
		return []string{"repo"}
	case "synchronization", "synchronise", "synchronize":
		return []string{"sync"}
	default:
		return nil
	}
}

var codeTopicStopWords = map[string]bool{
	"all": true, "and": true, "are": true, "code": true, "for": true,
	"how": true, "in": true, "into": true, "is": true, "of": true,
	"or": true, "responsible": true, "show": true, "the": true,
	"this": true, "to": true, "where": true, "who": true,
}
