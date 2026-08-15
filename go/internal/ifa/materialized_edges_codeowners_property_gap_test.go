// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import "testing"

// TestExpectedEdgeDetectsCodeownersPropertyCorruption is a deliberate RED test
// (#5992) proving a structural gap in the SHARED materialized-edge
// exhaustiveness mechanism (ExpectedEdge/Key(), materialized_edges_assert.go)
// before any codeowners_ownership_edges fixture is built on top of it.
//
// A DECLARES_CODEOWNER relationship's true graph identity is
// (repo, team, pattern, source_path): canonical_codeowners_edges.go's write
// template keys the relationship MERGE itself on
// `[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]`,
// specifically so one team named as owner by two different CODEOWNERS rule
// patterns (a routine real-world shape — a team commonly owns several globs)
// produces two DISTINCT relationships between the SAME (repo, team) node
// pair, not one relationship whose properties get overwritten.
//
// ExpectedEdge's identity triple is only
// (RelationshipType, SourceEntityID, TargetEntityID) — see
// materialized_edges_assert.go's doc comment: it "carries NO relationship
// properties" by design. Every family's guard and cmd/ifa's live
// `assert-edges` verb both count edges by ExpectedEdge.Key() alone (the same
// algorithm: build a want/got multiset keyed by Key(), diff missing/extra —
// see compareCodeCallExpectedEdges in materialized_edges_code_calls.go and
// assertMaterializedEdges in cmd/ifa/assert_edges.go). That comparison can
// prove "N DECLARES_CODEOWNER edges exist between repo-x and org/docs", but it
// cannot prove WHICH pattern/source_path/order_index each of those N edges
// carries — a materialization bug that cross-wires those properties between
// two rows sharing a (repo, team) pair changes no ExpectedEdge.Key(), so the
// exact-set comparison reports zero mismatch.
//
// This is a real correctness bug's failure mode, not a contrived one: a caller
// asking "which pattern does org/docs own via the second CODEOWNERS rule"
// would get a wrong answer while count(:DECLARES_CODEOWNER edges to org/docs)
// stayed correct.
//
// This gap is NOT unique to codeowners — the same relationship-MERGE-keyed-on-
// a-property shape exists for submodule_pin_edges
// (canonical_submodule_edges.go's `[rel:PINS_SUBMODULE {path: ...}]`), so this
// is a systemic limitation of the shared ExpectedEdge mechanism as designed
// for #5543, not something this family's fixture, guard, or cassette can
// self-correct: materialized_edges_assert.go is out of #5992's scope this
// phase (owned by the #5543 umbrella coordinator alongside the concurrent
// #5998 rationale_edges work). See the codeowners family guard's own doc
// comment for how its Odù is scoped around this known, reported limitation.
func TestExpectedEdgeDetectsCodeownersPropertyCorruption(t *testing.T) {
	t.Parallel()

	// codeownersRule is the property-bearing shape a real
	// codeowners.ownership-derived row carries
	// (ExtractCodeownersOwnershipEdgeRowsWithQuarantine's "pattern",
	// "source_path", "order_index" fields) — everything ExpectedEdge drops.
	type codeownersRule struct {
		pattern, sourcePath string
		orderIndex          int
	}

	// The correct graph truth: team "org/docs" owns two distinct CODEOWNERS
	// rules in the same repository — a routine, not a degenerate, shape.
	correctRuleA := codeownersRule{pattern: "*.md", sourcePath: ".github/CODEOWNERS", orderIndex: 1}
	correctRuleB := codeownersRule{pattern: "docs/**", sourcePath: ".github/CODEOWNERS", orderIndex: 3}

	// The corrupted graph truth: a materialization bug cross-wired rule A's
	// and rule B's pattern/source_path/order_index between the two written
	// rows. Both edges still exist and both still connect the same (repo,
	// team) pair — only WHICH rule contributed WHICH edge is wrong, which is
	// a real defect a reader of either rule's evidence would see.
	corruptedRuleA := correctRuleB
	corruptedRuleB := correctRuleA
	if corruptedRuleA == correctRuleA {
		t.Fatal("test setup bug: the corrupted fixture must differ from the correct one")
	}

	// Project both onto ExpectedEdge — the SAME lossy (type, source, target)
	// triple every family's hand-derived fixture and the live assert-edges
	// verb key their comparison on. Two rows in each set share the identical
	// (repo, team) endpoints by construction.
	edgesFor := func(_, _ codeownersRule) []ExpectedEdge {
		edge := ExpectedEdge{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: "org/docs"}
		return []ExpectedEdge{edge, edge}
	}
	expected := edgesFor(correctRuleA, correctRuleB)
	actual := edgesFor(corruptedRuleA, corruptedRuleB)

	// This is exactly the multiset-by-Key() algorithm both
	// compareCodeCallExpectedEdges and cmd/ifa's assertMaterializedEdges run:
	// count occurrences of each Key(), then require every expected count to be
	// met exactly.
	want := make(map[string]int, len(expected))
	for _, e := range expected {
		want[e.Key()]++
	}
	got := make(map[string]int, len(actual))
	for _, e := range actual {
		got[e.Key()]++
	}

	// This is the RED assertion: a property-aware comparison MUST be able to
	// tell the correct rule assignment apart from the cross-wired one. It
	// fails today because want and got are identical multisets — the
	// documented gap above, proven concretely.
	if mapsEqual(want, got) {
		t.Fatalf(
			"ExpectedEdge-keyed exact-set comparison cannot distinguish the CORRECT codeowners rule assignment (%+v, %+v) from the PROPERTY-CORRUPTED one (%+v, %+v) — both produce the identical ExpectedEdge multiset %v; a materialization bug that cross-wires pattern/source_path/order_index between two rules owned by the same team is silently certified as correct by the shared ExpectedEdge mechanism",
			correctRuleA, correctRuleB, corruptedRuleA, corruptedRuleB, want,
		)
	}
}

// mapsEqual reports whether two edge-key multisets are identical.
func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestExpectedEdgeDetectsMissingEdgeMaskedByUnrelatedDuplicate is the second
// half of the gap TestExpectedEdgeDetectsCodeownersPropertyCorruption proves
// (#5992, per the coordinator's follow-up: "it is not only 'a corrupted
// property passes', it is 'two legitimately distinct edges are
// indistinguishable, and one silently missing edge can be masked by the
// other'").
//
// codeownersFamilyOdu's RULE A, RULE B, and RULE C's second owner token are
// three DISTINCT DECLARES_CODEOWNER relationships between repo-x and
// org/docs (three different (pattern, source_path) pairs). This test proves
// a graph that DROPPED one of those three real relationships (a
// materialization bug: RULE B's edge never got written) but ALSO
// double-wrote a different one (RULE A's edge written twice by an unrelated
// concurrent-MERGE race) is INDISTINGUISHABLE from the correct graph by the
// exact-set ExpectedEdge comparison: both have exactly 3 edges keyed
// DECLARES_CODEOWNER|repo-x|org/docs. Two independent, unrelated defects --
// a missing edge and a duplicate edge -- net to the same total and cancel
// each other out in the multiset comparison every family's guard and the
// live assert-edges verb both run.
func TestExpectedEdgeDetectsMissingEdgeMaskedByUnrelatedDuplicate(t *testing.T) {
	t.Parallel()

	docsEdge := ExpectedEdge{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: "org/docs"}

	// Correct graph truth: three distinct rules (*.md, docs/**, *.go's
	// second owner) each contribute one edge to org/docs.
	correct := []ExpectedEdge{docsEdge, docsEdge, docsEdge}

	// Corrupted graph truth: RULE B's edge (docs/**) was never written (a
	// real materialization defect), but RULE A's edge (*.md) was written
	// TWICE by an unrelated duplicate-write defect (a concurrent-MERGE race
	// -- see cmd/ifa's own TestAssertMaterializedEdgesDuplicateEdgeFails for
	// why that class is a real, previously-seen regression, #5549). Same
	// relationship type, same two endpoints on every row, so this projects
	// to the identical ExpectedEdge triple three times either way.
	corrupted := []ExpectedEdge{docsEdge, docsEdge, docsEdge}

	want := make(map[string]int, len(correct))
	for _, e := range correct {
		want[e.Key()]++
	}
	got := make(map[string]int, len(corrupted))
	for _, e := range corrupted {
		got[e.Key()]++
	}

	// This is the RED assertion: a comparison that could actually tell "one
	// dropped + one duplicated" apart from "three genuine edges" would find
	// a difference here. It does not, because ExpectedEdge carries no
	// per-rule identity (no pattern/source_path) to tell the three rows
	// apart in the first place -- the missing RULE B edge and the duplicate
	// RULE A edge are simply the same key counted the same number of times.
	if mapsEqual(want, got) {
		t.Fatalf(
			"exact-set ExpectedEdge comparison cannot distinguish 3 GENUINE org/docs edges (RULE A, RULE B, RULE C) from 2 real edges plus 1 unrelated duplicate (RULE B dropped, RULE A double-written) -- both multisets are %v; a dropped rule and an unrelated duplicate-write defect mask each other completely",
			want,
		)
	}
}
