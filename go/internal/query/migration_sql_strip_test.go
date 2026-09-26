// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestStripSQLCommentsHidesCommentedFragments is the seeded RED/GREEN pair for
// the migration binding helper: a fragment that appears only inside a comment
// must not satisfy a binding, while the same fragment in live SQL must.
func TestStripSQLCommentsHidesCommentedFragments(t *testing.T) {
	t.Parallel()

	const fragment = "fact_kind IN ('a', 'b', 'c')"
	for name, tc := range map[string]struct {
		sql  string
		want bool
	}{
		"line comment only":      {"CREATE INDEX i ON t (x) WHERE fact_kind IN ('a'); -- " + fragment + "\n", false},
		"full line comment only": {"-- " + fragment + "\nCREATE INDEX i ON t (x);", false},
		"block comment only":     {"CREATE INDEX i ON t (x) /* " + fragment + " */;", false},
		"nested block comment":   {"/* outer /* inner */ " + fragment + " */ CREATE INDEX i ON t (x);", false},
		"live predicate":         {"CREATE INDEX i ON t (x) WHERE " + fragment + ";", true},
		"live plus comment":      {"-- note\nCREATE INDEX i ON t (x) WHERE " + fragment + "; -- tail\n", true},
		"dashes in string":       {"SELECT '-- " + fragment + "';", true},
		"escaped quote string":   {"SELECT 'it''s -- " + fragment + "';", true},
		"block marker in string": {"SELECT '/* " + fragment + " */';", true},
		"dashes in identifier":   {"SELECT \"a--" + fragment + "\";", true},
	} {
		got := strings.Contains(normalizeSQLWhitespace(stripSQLComments(tc.sql)), fragment)
		if got != tc.want {
			t.Errorf("%s: fragment visible after stripping = %v, want %v\nstripped: %q", name, got, tc.want, stripSQLComments(tc.sql))
		}
	}
}

// TestStripSQLCommentsKeepsTokensApart guards against a comment gluing the
// tokens on either side of it into one identifier.
func TestStripSQLCommentsKeepsTokensApart(t *testing.T) {
	t.Parallel()

	got := normalizeSQLWhitespace(stripSQLComments("WHERE/* c */is_tombstone -- x\n= FALSE"))
	if want := "WHERE is_tombstone = FALSE"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}
