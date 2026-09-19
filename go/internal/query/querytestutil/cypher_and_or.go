// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"regexp"
	"testing"
)

// brokenAndOrPattern matches an AND/OR keyword immediately preceded by a
// newline or tab. NornicDB v1.3.3 mis-evaluates the whole WHERE clause when
// this happens (issue #6786 shape X4, proven live with the schema applied
// and the Go driver): the character immediately before AND/OR must be a
// space, not a bare newline or tab from gofmt-style multi-line formatting.
// "\n  AND" and "\n\t AND" (a space after the newline/tab) are fine;
// "\n\t\t\tAND" and "\nAND" are not. The word boundary (\b) after AND/OR
// keeps this from matching inside another identifier, e.g. "\nORDER BY".
var brokenAndOrPattern = regexp.MustCompile(`[\n\t](AND|OR)\b`)

// CypherHasBrokenAndOr reports whether cypher contains an AND/OR keyword
// immediately preceded by a newline or tab -- see brokenAndOrPattern's doc
// comment for the NornicDB v1.3.3 defect this guards (#6786 X4).
func CypherHasBrokenAndOr(cypher string) bool {
	return brokenAndOrPattern.MatchString(cypher)
}

// AssertCypherHasNoBrokenAndOr fails tb (via Fatalf) if cypher contains the
// NornicDB v1.3.3 AND/OR-immediately-after-newline-or-tab defect shape
// (issue #6786 X4). Use this on the exact rendered/captured production
// Cypher text, not a hand-written approximation of it.
func AssertCypherHasNoBrokenAndOr(tb testing.TB, cypher string) {
	tb.Helper()
	if loc := brokenAndOrPattern.FindString(cypher); loc != "" {
		tb.Fatalf("cypher contains %q immediately after a newline or tab, which NornicDB v1.3.3 mis-evaluates as a broken WHERE clause (issue #6786 X4):\n%s", loc, cypher)
	}
}
