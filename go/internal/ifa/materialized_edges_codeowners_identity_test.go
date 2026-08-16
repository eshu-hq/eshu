// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// declaresCodeowner builds the ExpectedEdge one CODEOWNERS rule projects to.
// A blank pattern and sourcePath yield an Identity-free edge, which is exactly
// the triple-only shape ExpectedEdge carried before the relationship-identity
// properties landed. Both tests below run the identity-bearing and the
// triple-only shape through the SAME Key(), so each one's control arm proves
// the positive assertion has teeth instead of restating a tautology.
func declaresCodeowner(target, pattern, sourcePath string) ExpectedEdge {
	edge := ExpectedEdge{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: target}
	if pattern != "" || sourcePath != "" {
		edge.Identity = map[string]string{"pattern": pattern, "source_path": sourcePath}
	}
	return edge
}

// edgeKeyMultiset is the exact multiset-by-Key() algorithm every family's
// vacuity guard and cmd/ifa's assertMaterializedEdges both run. Both tests
// call this one helper so what they assert is the real comparison rather than
// a re-implementation of it.
func edgeKeyMultiset(edges []ExpectedEdge) map[string]int {
	out := make(map[string]int, len(edges))
	for _, e := range edges {
		out[e.Key()]++
	}
	return out
}

func edgeKeyMultisetsEqual(a, b map[string]int) bool {
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

// TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate proves
// the shared exhaustiveness mechanism can see one silently missing codeowners
// edge even when an unrelated duplicate masks the count.
//
// A DECLARES_CODEOWNER relationship's true graph identity is
// (repo, team, pattern, source_path): canonical_codeowners_edges.go's write
// template keys the relationship MERGE itself on
// `[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]`,
// specifically so one team named as owner by several CODEOWNERS rule patterns
// -- a routine real-world shape -- produces several DISTINCT relationships
// between the SAME (repo, team) node pair rather than one relationship whose
// properties get overwritten.
//
// codeownersFamilyOdu's RULE A, RULE B, and RULE C's second owner token are
// three such relationships. A graph that DROPPED RULE B's edge but ALSO
// double-wrote RULE A's (a concurrent-MERGE duplicate, the class cmd/ifa's
// TestAssertMaterializedEdgesDuplicateEdgeFails exists for, #5549) still holds
// exactly three edges between that endpoint pair: two independent defects that
// net to the same total and, under a triple-only key, cancel out completely.
// The declared identity is what keeps them apart.
func TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate(t *testing.T) {
	t.Parallel()

	const source = ".github/CODEOWNERS"

	correct := []ExpectedEdge{
		declaresCodeowner("org/docs", "*.md", source),
		declaresCodeowner("org/docs", "docs/**", source),
		declaresCodeowner("org/docs", "*.go", source),
	}
	// RULE B (docs/**) was never written; RULE A (*.md) was written twice.
	corrupted := []ExpectedEdge{
		declaresCodeowner("org/docs", "*.md", source),
		declaresCodeowner("org/docs", "*.md", source),
		declaresCodeowner("org/docs", "*.go", source),
	}

	if edgeKeyMultisetsEqual(edgeKeyMultiset(correct), edgeKeyMultiset(corrupted)) {
		t.Fatalf(
			"ExpectedEdge.Key() cannot distinguish three genuine org/docs edges from two real edges plus one unrelated duplicate; both render the identical multiset %v",
			edgeKeyMultiset(correct),
		)
	}

	// Control: strip the declared identity and the dropped rule and the
	// duplicate cancel out exactly, collapsing all three rows onto one key.
	// This is what the Identity component buys, and it is why relaxing the
	// fixture's identity keys would silently re-open the gap.
	tripleOnly := []ExpectedEdge{
		declaresCodeowner("org/docs", "", ""),
		declaresCodeowner("org/docs", "", ""),
		declaresCodeowner("org/docs", "", ""),
	}
	if got := len(edgeKeyMultiset(tripleOnly)); got != 1 {
		t.Fatalf("control arm is wrong: three identity-free edges rendered %d distinct keys, want 1", got)
	}
}

// TestCodeownersOwnershipIdentityExcludesOrderIndex pins the exact boundary of
// what this family's exhaustiveness proof does and does not cover, in both
// directions, so nobody later reads the guard as stronger than it is.
//
// The write template SETs order_index and generation_id on the relationship
// but does NOT fold either into the MERGE key
// (canonical_codeowners_edges.go). Two rules that differ only in order_index
// are therefore the SAME graph edge as far as identity is concerned, and the
// guard projects them to the same key on purpose
// (codeownersOwnershipRowsToExpectedEdges). One consequence is worth stating
// plainly: a materialization bug that swapped two same-team rules' order_index
// values would change no edge identity and would not be caught here -- that is
// a limit of the write template's identity, not of the assertion, and closing
// it would mean widening the MERGE key, not the fixture.
//
// The registry half of the assertion is what makes this a lockstep guard
// rather than a comment: adding order_index to
// cypher.MaterializedEdgeIdentityProperties without updating the row
// projection (or the reverse) turns this red.
func TestCodeownersOwnershipIdentityExcludesOrderIndex(t *testing.T) {
	t.Parallel()

	declared, err := cypher.MaterializedEdgeIdentityProperties(codeownersOwnershipFamily)
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(%s): %v", codeownersOwnershipFamily, err)
	}
	got := declared["DECLARES_CODEOWNER"]
	want := []string{"pattern", "source_path"}
	if !slices.Equal(got, want) {
		t.Fatalf("DECLARES_CODEOWNER identity properties = %v, want %v; the row projection in codeownersOwnershipRowsToExpectedEdges must be updated in lockstep with this registry entry", got, want)
	}

	rows := []map[string]any{
		{"repo_id": "repo-x", "owner_ref": "org/docs", "pattern": "*.md", "source_path": ".github/CODEOWNERS", "order_index": 1},
		{"repo_id": "repo-x", "owner_ref": "org/docs", "pattern": "*.md", "source_path": ".github/CODEOWNERS", "order_index": 7},
	}
	edges := codeownersOwnershipRowsToExpectedEdges(rows)
	if len(edges) != 2 {
		t.Fatalf("codeownersOwnershipRowsToExpectedEdges produced %d edge(s) for 2 rows, want 2", len(edges))
	}
	if edges[0].Key() != edges[1].Key() {
		t.Fatalf("two rows differing only in order_index rendered different keys (%q vs %q); order_index is a SET-only property and must not participate in edge identity", edges[0].Key(), edges[1].Key())
	}
	for i, edge := range edges {
		if _, present := edge.Identity["order_index"]; present {
			t.Errorf("edge %d carries order_index in its Identity map; LoadExpectedEdges rejects undeclared identity keys, so the live and offline sides would disagree", i)
		}
	}
}
