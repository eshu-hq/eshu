// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

const readOnlyCypherCapability = "graph_query.read_only_cypher"

// visualizationGraphQueryCapability is the capability for visualize_graph_query.
// The tool executes a bounded read-only Cypher query and projects the graph
// entities in the result into a renderable subgraph, so it is graph-backed and
// shares the authoritative-graph profile gating of read-only Cypher.
const visualizationGraphQueryCapability = "visualization.graph_query"

// cypherMutationKeywords are keywords that indicate a write or destructive
// Cypher operation. We reject these as tokens regardless of position so that
// even obfuscated or commented-out mutations are blocked.
var cypherMutationKeywords = []string{
	"CREATE", "MERGE", "DELETE", "DETACH", "SET", "REMOVE",
	"DROP", "CALL", "FOREACH",
}

var cypherMutationKeywordPhrases = []cypherKeywordPhrase{
	{display: "LOAD CSV", terms: []string{"LOAD", "CSV"}},
}

type cypherKeywordPhrase struct {
	display string
	terms   []string
}

// validateReadOnlyCypher returns an error if the query appears to contain
// write or administrative operations. The Neo4j driver session is also
// opened with AccessModeRead as a second line of defense, but we reject
// obvious mutations before they reach the driver.
func validateReadOnlyCypher(cypher string) error {
	if len(cypher) > cypherMaxQueryLength {
		return fmt.Errorf("query exceeds maximum length of %d characters", cypherMaxQueryLength)
	}

	for _, kw := range cypherMutationKeywords {
		if containsCypherKeyword(cypher, kw) {
			return fmt.Errorf("query contains disallowed keyword %q; only read-only queries are permitted", kw)
		}
	}
	for _, phrase := range cypherMutationKeywordPhrases {
		if containsCypherKeywordPhrase(cypher, phrase.terms) {
			return fmt.Errorf("query contains disallowed keyword %q; only read-only queries are permitted", phrase.display)
		}
	}

	return nil
}

func containsCypherKeyword(query, keyword string) bool {
	for index := 0; index < len(query); {
		token, next, ok := nextCypherIdentifierToken(query, index)
		if !ok {
			return false
		}
		if strings.EqualFold(token, keyword) {
			return true
		}
		index = next
	}
	return false
}

func containsCypherKeywordPhrase(query string, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	for index := 0; index < len(query); {
		token, next, ok := nextCypherIdentifierToken(query, index)
		if !ok {
			return false
		}
		index = next
		if !strings.EqualFold(token, terms[0]) {
			continue
		}
		matched := true
		phraseIndex := index
		for _, term := range terms[1:] {
			nextToken, after, found := nextCypherIdentifierToken(query, phraseIndex)
			if !found || !strings.EqualFold(nextToken, term) {
				matched = false
				break
			}
			phraseIndex = after
		}
		if matched {
			return true
		}
	}
	return false
}

func nextCypherIdentifierToken(query string, index int) (string, int, bool) {
	for index < len(query) && !isCypherIdentifierChar(query[index]) {
		index++
	}
	if index >= len(query) {
		return "", index, false
	}
	start := index
	for index < len(query) && isCypherIdentifierChar(query[index]) {
		index++
	}
	return query[start:index], index, true
}
