// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox

import "strings"

// cypherDenylist is the set of Cypher write/DDL/side-effect clause keywords
// that are forbidden in read-only sandboxed queries. Each key is uppercase.
// Matching is whole-word only (see tokenizeMasked): a relationship type or
// identifier that CONTAINS one of these strings as a substring — e.g.
// `:CALLS`, `:CREATES`, a property named `deleted` — must NOT be denied.
//
// Denylist tokens match identifier tokens in ANY syntactic position (clause
// keyword, relationship type, label, or property key). Under the v1
// default-deny policy a read query that uses a denylist word as a relationship
// type or label (e.g. `[:CALL]`, `[:SET]`) is denied; this is an intentional
// safe-side false positive, not a bug.
//
// v1 policy: CALL is denied unconditionally because Cypher procedures can
// perform mutations and there is no pure-read procedure allowlist yet.
var cypherDenylist = map[string]struct{}{
	"CREATE":  {},
	"MERGE":   {},
	"DELETE":  {},
	"SET":     {},
	"REMOVE":  {},
	"DROP":    {},
	"FOREACH": {},
	"CALL":    {},
	"LOAD":    {}, // covers LOAD CSV
	"DETACH":  {},
}

// Bounded deny-reason prefixes. These strings never echo the query body.
const (
	reasonMultipleStatements = "multiple statements not permitted"
	reasonEmptyQuery         = "empty query"
	reasonNoReturn           = "query must return rows"
	reasonWriteClause        = "write clause not permitted: "
	reasonQueryRejected      = "query rejected: "
)

// validateCypher returns a Decision for a Cypher query under the read-only
// sandbox policy. It is the single entry point for Cypher query authorization.
//
// Precondition: callers must enforce Caps.MaxQueryLen before calling;
// validateCypher does not length-check. Control-character rejection (bytes
// < 0x20 other than TAB/LF/CR, and DEL 0x7F) is handled inside normalize and
// propagated as a deny reason — callers do not need to pre-screen for those.
//
// Policy (evaluated in order):
//  1. normalize is called; a normalize error → deny with a bounded reason.
//  2. statementCount != 1 → deny (covers empty queries and stacked statements).
//  3. The masked code is tokenized on non-identifier characters; any token that
//     matches a cypherDenylist keyword (case-insensitive, exact whole-word match)
//     → deny naming the keyword in the reason.
//  4. The masked token set must contain a RETURN token → a read query returns rows.
//  5. All checks passed → allow.
//
// The reason string on deny is always bounded: it never echoes the query body
// and uses only fixed prefixes plus a single keyword or normalizer error string.
func validateCypher(query string) Decision {
	n := normalize(DialectCypher, query)
	if n.err != "" {
		return Decision{Allowed: false, Reason: reasonQueryRejected + n.err}
	}
	if n.statementCount == 0 {
		return Decision{Allowed: false, Reason: reasonEmptyQuery}
	}
	if n.statementCount != 1 {
		return Decision{Allowed: false, Reason: reasonMultipleStatements}
	}

	var hasReturn bool
	for _, tok := range tokenizeMasked(n.masked) {
		upper := strings.ToUpper(tok)
		if _, denied := cypherDenylist[upper]; denied {
			return Decision{Allowed: false, Reason: reasonWriteClause + upper}
		}
		if upper == "RETURN" {
			hasReturn = true
		}
	}

	if !hasReturn {
		return Decision{Allowed: false, Reason: reasonNoReturn}
	}
	return Decision{Allowed: true}
}

// tokenizeMasked splits the masked query string into word tokens by scanning
// left-to-right and collecting runs of identifier characters ([A-Za-z0-9_]).
// Any byte that is not an identifier character acts as a delimiter and is
// skipped. This ensures that a relationship type `:CALLS` produces the single
// token `CALLS`, which does NOT match the denylist keyword `CALL`.
func tokenizeMasked(masked string) []string {
	tokens := make([]string, 0, 32)
	start := -1
	for i := 0; i < len(masked); i++ {
		b := masked[i]
		if isIdentChar(b) {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				tokens = append(tokens, masked[start:i])
				start = -1
			}
		}
	}
	if start != -1 {
		tokens = append(tokens, masked[start:])
	}
	return tokens
}

// isIdentChar reports whether b is an identifier character: [A-Za-z0-9_].
func isIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
