// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import "strings"

// pilotSQLTableAliases returns only aliases bound by the current SELECT.
// Parenthesized subqueries have a separate scope and are deliberately skipped.
func pilotSQLTableAliases(tokens []string) map[string]bool {
	aliases := make(map[string]bool)
	depth := 0
	for i, token := range tokens {
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth != 0 || token != "FROM" && token != "JOIN" || i+1 >= len(tokens) {
			continue
		}
		name := i + 1
		if !pilotValidName(tokens[name]) {
			continue
		}
		if name+2 < len(tokens) && tokens[name+1] == "." && pilotValidName(tokens[name+2]) {
			name += 2
		}
		if name+2 < len(tokens) && tokens[name+1] == "AS" && pilotValidName(tokens[name+2]) {
			aliases[tokens[name+2]] = true
		} else {
			aliases[tokens[name]] = true
		}
	}
	return aliases
}

// A correlated grant needs at least one identifier from each SELECT scope.
// An inner declaration that shadows an outer alias makes the probe independent
// of the outer row and must fail even when the predicate text is unchanged.
func pilotSQLCorrelationLinksScopes(pattern []string, outer, inner map[string]bool) bool {
	seen := make(map[string]bool)
	outerReference, innerReference := false, false
	for i := 0; i+2 < len(pattern); i++ {
		if pattern[i+1] != "." || !pilotValidName(pattern[i]) {
			continue
		}
		alias := strings.ToUpper(pattern[i])
		if seen[alias] {
			continue
		}
		seen[alias] = true
		if outer[alias] == inner[alias] {
			return false
		}
		outerReference = outerReference || outer[alias]
		innerReference = innerReference || inner[alias]
	}
	return outerReference && innerReference
}

// pilotSQLGrantJoinBound requires the grant scope row to belong to the fact
// selected for this owner. The equality must dominate the scope JOIN's ON
// expression; an OR branch can otherwise turn a valid grant into a cross join.
func pilotSQLGrantJoinBound(subquery []string, scopeAlias, factAlias string) bool {
	scopeAlias, factAlias = strings.ToUpper(scopeAlias), strings.ToUpper(factAlias)
	depth := 0
	for i, token := range subquery {
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth != 0 || token != "JOIN" {
			continue
		}
		j := i + 1
		if j >= len(subquery) || !pilotValidName(subquery[j]) {
			continue
		}
		j++
		alias := subquery[j-1]
		if j+1 < len(subquery) && subquery[j] == "AS" {
			alias = subquery[j+1]
			j += 2
		}
		if alias != scopeAlias || j >= len(subquery) || subquery[j] != "ON" {
			continue
		}
		start, end, onDepth := j+1, len(subquery), 0
		for k := start; k < len(subquery); k++ {
			switch subquery[k] {
			case "(", "[", "{":
				onDepth++
			case ")", "]", "}":
				onDepth--
			}
			if onDepth == 0 && (subquery[k] == "JOIN" || subquery[k] == "WHERE" || subquery[k] == "LIMIT") {
				end = k
				break
			}
		}
		forward := []string{scopeAlias, ".", "SCOPE_ID", "=", factAlias, ".", "SCOPE_ID"}
		reverse := []string{factAlias, ".", "SCOPE_ID", "=", scopeAlias, ".", "SCOPE_ID"}
		on := subquery[start:end]
		return pilotGuarantees(on, "", forward) || pilotGuarantees(on, "", reverse)
	}
	return false
}
