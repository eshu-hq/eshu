// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "testing"

// TestCypherHasBrokenAndOr is the seeded RED/GREEN pair issue #6786 review
// F5 requires for this guard: it must fire on a planted violation (RED) and
// pass on the allowed forms the doc comment claims are fine (GREEN), not
// merely look correct on inspection.
func TestCypherHasBrokenAndOr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cypher  string
		broken  bool
		comment string
	}{
		// RED: seeded violations the guard must catch.
		{
			name:    "tab-indented AND after newline",
			cypher:  "MATCH (n:Workload) WHERE n.id = $x\n\t\t\tAND n.repo_id = $r",
			broken:  true,
			comment: "gofmt-style multi-line WHERE with AND on its own indented line",
		},
		{
			name:    "bare newline before AND",
			cypher:  "MATCH (n) WHERE n.id = $x\nAND n.repo_id = $r",
			broken:  true,
			comment: "no indentation at all before AND",
		},
		{
			name:    "bare newline before OR",
			cypher:  "MATCH (n) WHERE n.id = $x\nOR n.id = $y",
			broken:  true,
			comment: "OR is affected the same way AND is",
		},
		{
			name:    "single tab before OR",
			cypher:  "MATCH (n) WHERE n.id = $x\n\tOR n.id = $y",
			broken:  true,
			comment: "a lone tab (no trailing space) still triggers it",
		},
		// GREEN: allowed forms the doc comment explicitly claims are fine.
		{
			name:    "newline then two spaces before AND",
			cypher:  "MATCH (n) WHERE n.id = $x\n  AND n.repo_id = $r",
			broken:  false,
			comment: "space immediately before AND",
		},
		{
			name:    "tab then space before AND",
			cypher:  "MATCH (n) WHERE n.id = $x\n\t AND n.repo_id = $r",
			broken:  false,
			comment: "a space after the tab, immediately before AND",
		},
		{
			name:    "AND on the same line as its left operand",
			cypher:  "WHERE %s AND %s",
			broken:  false,
			comment: "repositoryDependencyClusterEdgeCypher's scoped join shape",
		},
		{
			name:    "OR inside a single-line inline-map condition",
			cypher:  "(a.id IN $x OR a.id IN $y)",
			broken:  false,
			comment: "GraphConditionOnProperty's rendered shape",
		},
		{
			name:    "ORDER BY after a newline must not false-positive on OR",
			cypher:  "RETURN n.id\n\t\tORDER BY n.id",
			broken:  false,
			comment: "word boundary after OR/AND must not match inside ORDER",
		},
		{
			name:    "no AND or OR at all",
			cypher:  "MATCH (n:Repository {id: $id}) RETURN n.id",
			broken:  false,
			comment: "baseline: nothing to flag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CypherHasBrokenAndOr(tc.cypher); got != tc.broken {
				t.Fatalf("CypherHasBrokenAndOr(%q) = %v, want %v (%s)", tc.cypher, got, tc.broken, tc.comment)
			}
		})
	}
}

// fakeTB records whether Fatalf was called, without actually failing the
// enclosing test -- used to prove AssertCypherHasNoBrokenAndOr's RED
// (it calls Fatalf) and GREEN (it doesn't) behavior without a subtest that
// intentionally fails.
type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(string, ...any) {
	f.failed = true
}

func TestAssertCypherHasNoBrokenAndOr(t *testing.T) {
	t.Parallel()

	t.Run("RED: fails on a seeded violation", func(t *testing.T) {
		fake := &fakeTB{}
		AssertCypherHasNoBrokenAndOr(fake, "MATCH (n) WHERE n.id = $x\n\t\t\tAND n.repo_id = $r")
		if !fake.failed {
			t.Fatal("AssertCypherHasNoBrokenAndOr did not fail on a seeded AND-after-tab violation")
		}
	})

	t.Run("GREEN: passes on the clean tree", func(t *testing.T) {
		fake := &fakeTB{}
		AssertCypherHasNoBrokenAndOr(fake, "MATCH (n) WHERE n.id = $x\n  AND n.repo_id = $r")
		if fake.failed {
			t.Fatal("AssertCypherHasNoBrokenAndOr failed on a clean (space-preceded AND) statement")
		}
	})
}
