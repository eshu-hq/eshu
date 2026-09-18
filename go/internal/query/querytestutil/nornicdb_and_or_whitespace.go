// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"regexp"
	"testing"
)

// nornicdbBrokenAndOrPattern matches an AND or OR keyword whose immediately
// preceding character is a newline or a tab rather than a space -- the exact
// shape proven live to break NornicDB v1.3.3 (#6786,
// docs/internal/evidence/6786-scoped-grant-nornicdb-read-predicates.md): on
// that pinned image, `WHERE x = $a\n\tAND y = $b` (or `\nAND`, with no space
// immediately before AND/OR) can silently drop the WHOLE WHERE clause, not
// just the AND/OR term itself. `\n  AND` and `\n\t AND` (a space directly
// before AND/OR, however the line got there) are unaffected.
var nornicdbBrokenAndOrPattern = regexp.MustCompile(`[\n\t](AND|OR)\b`)

// AssertCypherHasNoBrokenAndOr fails t if cypher contains an AND/OR keyword
// immediately preceded by a newline or tab instead of a space. Call it with
// the exact production-rendered Cypher text a test's graph-reader double
// captured, on every scoped-grant call site: a caller that reintroduces a
// multi-line `AND (...)`/`OR (...)` group without a leading space is
// reintroducing the #6786 NornicDB v1.3.3 defect, and this assertion catches
// it at the Go-string level, before a live backend has to.
func AssertCypherHasNoBrokenAndOr(t *testing.T, cypher string) {
	t.Helper()
	if loc := nornicdbBrokenAndOrPattern.FindStringIndex(cypher); loc != nil {
		t.Fatalf(
			"cypher has %q directly after a newline/tab with no leading space -- NornicDB v1.3.3 can silently drop the WHOLE WHERE clause this sits in (#6786):\n%s",
			cypher[loc[0]:loc[1]], cypher,
		)
	}
}
