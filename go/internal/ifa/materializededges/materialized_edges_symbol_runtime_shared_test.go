// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSymbolRuntimeExpectedEdgesFixture writes edges to a fresh
// expected-edge-set fixture file under t.TempDir(), through the SAME
// sqlRelationshipExpectedEdge/sqlRelationshipExpectedEdgesFile shape
// LoadExpectedEdges reads back (materialized_edges_sql.go) -- so every
// perturbation this package's handles_route/runs_in/invokes_cloud_action
// negative tests exercise goes through the real JSON field names
// ("relationship_type", "source_entity_id", ...), not a hand-typed
// approximation of them. It NEVER writes into a committed testdata/ path;
// every caller perturbs a copy loaded from the real fixture first.
func writeSymbolRuntimeExpectedEdgesFixture(t *testing.T, edges []ExpectedEdge) string {
	t.Helper()
	converted := make([]sqlRelationshipExpectedEdge, len(edges))
	for i, e := range edges {
		converted[i] = sqlRelationshipExpectedEdge(e)
	}
	raw, err := json.Marshal(sqlRelationshipExpectedEdgesFile{
		Odu:   "odu:ifa-symbol-runtime-family",
		Edges: converted,
	})
	if err != nil {
		t.Fatalf("marshal expected edges fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write expected edges fixture: %v", err)
	}
	return path
}

// flipOneHexChar mutates one hex digit of s to a different hex digit,
// scanning from the end so it reliably lands inside a hashed uid suffix
// (e.g. "content-entity:e_b802e874e4a3") rather than a literal prefix. This
// is exactly what a wrong content.CanonicalEntityID derivation looks like in
// production: a single differing character in an otherwise well-formed hash,
// not a structurally different string a naive check might catch by shape
// alone. t.Fatal if s has no hex character to flip (a caller bug: every
// SourceEntityID this package's fixtures use is a canonical Function uid
// ending in a hex-derived hash).
func flipOneHexChar(t *testing.T, s string) string {
	t.Helper()
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		var repl byte
		switch {
		case c >= '0' && c <= '8':
			repl = c + 1
		case c == '9':
			repl = 'a'
		case c >= 'a' && c <= 'e':
			repl = c + 1
		case c == 'f':
			repl = '0'
		default:
			continue
		}
		return s[:i] + string(repl) + s[i+1:]
	}
	t.Fatalf("flipOneHexChar: no hex character found to flip in %q", s)
	return ""
}

// corruptTargetEntityID appends a fixed, deterministic suffix so a target id
// (an Endpoint hash, a workload id, or a CloudAction id -- none of which are
// guaranteed to contain a hex digit, unlike a Function uid) becomes a
// different string with no risk of accidentally colliding with any real id
// these families' fixtures resolve.
func corruptTargetEntityID(s string) string {
	return s + "-corrupted-for-test"
}

// TestCompareSymbolRuntimeExpectedEdgesCatchesDroppedEdgeMaskedByDuplicate is
// the required mutation-proof of compareSymbolRuntimeExpectedEdges itself --
// the sole MISSING/EXTRA decision every one of the three symbol-runtime
// families' guards (resolveHandlesRouteMaterializedEdges,
// resolveRunsInMaterializedEdges, resolveInvokesCloudActionMaterializedEdges)
// delegates to. If this function were stubbed to return "" unconditionally,
// every `materialized_edges:*` coverage row for all three families would
// resolve covered forever. That would have been invisible to the rest of
// this package's committed suite before the resolve*-driven negative tests
// in materialized_edges_handles_route_test.go, materialized_edges_runs_in_test.go,
// and materialized_edges_invokes_cloud_action_test.go existed; those now
// fail through this function too, so the stub is caught there as well as
// here.
//
// The scenario mirrors TestCodeownersOwnershipFamilyGuardDetectsPropertyCorruption:
// one genuine edge (fn2 -> endpoint:bbb) never got produced, and an unrelated
// edge (fn1 -> endpoint:aaa) got produced TWICE instead. The two defects net
// to the SAME total edge count (2 want, 2 got), so a naive len()-only
// comparison would see nothing wrong -- this proves compareSymbolRuntimeExpectedEdges
// catches it as an identity mismatch in both directions at once: the dropped
// edge as MISSING, the duplicated edge as EXTRA.
func TestCompareSymbolRuntimeExpectedEdgesCatchesDroppedEdgeMaskedByDuplicate(t *testing.T) {
	t.Parallel()

	fn1Widgets := ExpectedEdge{RelationshipType: "HANDLES_ROUTE", SourceEntityID: "content-entity:fn1", TargetEntityID: "endpoint:aaa"}
	fn2Healthz := ExpectedEdge{RelationshipType: "HANDLES_ROUTE", SourceEntityID: "content-entity:fn2", TargetEntityID: "endpoint:bbb"}

	expected := []ExpectedEdge{fn1Widgets, fn2Healthz}
	// fn2Healthz never got produced; fn1Widgets got produced twice instead.
	actual := []ExpectedEdge{fn1Widgets, fn1Widgets}

	mismatch := compareSymbolRuntimeExpectedEdges("odu:probe", "handles route", expected, actual)
	if mismatch == "" {
		t.Fatal("compareSymbolRuntimeExpectedEdges did not catch a dropped edge masked by an unrelated duplicate; a stub returning \"\" unconditionally would pass every symbol-runtime coverage row forever")
	}
	if !strings.Contains(mismatch, "MISSING") {
		t.Errorf("mismatch %q does not name the MISSING fn2->endpoint:bbb edge", mismatch)
	}
	if !strings.Contains(mismatch, "EXTRA") {
		t.Errorf("mismatch %q does not name the EXTRA (duplicated) fn1->endpoint:aaa edge", mismatch)
	}
	if !strings.Contains(mismatch, "content-entity:fn2") {
		t.Errorf("mismatch %q does not name the specific dropped edge's source (content-entity:fn2)", mismatch)
	}
	if !strings.Contains(mismatch, "content-entity:fn1") {
		t.Errorf("mismatch %q does not name the specific duplicated edge's source (content-entity:fn1)", mismatch)
	}
	t.Logf("confirmed: compareSymbolRuntimeExpectedEdges catches a dropped edge masked by an unrelated duplicate: %s", mismatch)
}

// TestCompareSymbolRuntimeExpectedEdgesAcceptsAnExactMatch is the control arm
// for the mutation-proof above: the same two edges, produced exactly once
// each with no drop and no duplicate, must resolve clean. Without this
// control, a hypothetical over-eager comparator that always returns a
// non-empty string would also "pass" the dropped/duplicate case above by
// coincidence.
func TestCompareSymbolRuntimeExpectedEdgesAcceptsAnExactMatch(t *testing.T) {
	t.Parallel()

	edges := []ExpectedEdge{
		{RelationshipType: "HANDLES_ROUTE", SourceEntityID: "content-entity:fn1", TargetEntityID: "endpoint:aaa"},
		{RelationshipType: "HANDLES_ROUTE", SourceEntityID: "content-entity:fn2", TargetEntityID: "endpoint:bbb"},
	}
	if mismatch := compareSymbolRuntimeExpectedEdges("odu:probe", "handles route", edges, edges); mismatch != "" {
		t.Fatalf("compareSymbolRuntimeExpectedEdges(edges, edges) = %q, want \"\" for an exact match", mismatch)
	}
}
