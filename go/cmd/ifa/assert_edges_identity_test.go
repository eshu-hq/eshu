// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// codeownersIdentityForTest resolves codeowners_ownership_edges' declared
// identity the same way runAssertEdgesCommand does, so a drift in the
// registry's declared properties surfaces here too rather than only in the
// cypher package.
func codeownersIdentityForTest(t *testing.T) map[string][]string {
	t.Helper()
	identity, err := cypher.MaterializedEdgeIdentityProperties("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(codeowners_ownership_edges): %v", err)
	}
	return identity
}

func codeownersEdge(repoUID, teamUID, pattern, sourcePath string) graphdump.Edge {
	return graphdump.Edge{
		Type:      "DECLARES_CODEOWNER",
		FromProps: map[string]any{"id": repoUID},
		ToProps:   map[string]any{"uid": teamUID},
		Props:     map[string]any{"pattern": pattern, "source_path": sourcePath},
	}
}

// TestAssertMaterializedEdgesIdentityDistinguishesSameEndpointEdges proves the
// live comparison keys a declared-identity edge type by its identity
// properties, not just its endpoints: two DECLARES_CODEOWNER edges between the
// SAME (repo, team) pair but different (pattern, source_path) must both be
// counted, matching two distinct expected-set entries, rather than collapsing
// onto one key the way a bare (type, source, target) key would. This is
// exactly the collision codeowners' relationship-MERGE identity exists to
// prevent (canonical_codeowners_edges.go) — the live assertion has to prove
// the graph the same way.
func TestAssertMaterializedEdgesIdentityDistinguishesSameEndpointEdges(t *testing.T) {
	t.Parallel()

	types, err := ifa.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	identity := codeownersIdentityForTest(t)

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		codeownersEdge("repo-1", "team-a", "*.go", "CODEOWNERS"),
		codeownersEdge("repo-1", "team-a", "*.md", "docs/CODEOWNERS"),
	}}
	expected := []ifa.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a", Identity: map[string]string{"pattern": "*.go", "source_path": "CODEOWNERS"}},
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a", Identity: map[string]string{"pattern": "*.md", "source_path": "docs/CODEOWNERS"}},
	}

	if err := assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, identity, expected); err != nil {
		t.Fatalf("assertMaterializedEdges(distinct identity, same endpoints) = %v, want nil; the two rule declarations must not collapse onto one key", err)
	}
}

// TestAssertMaterializedEdgesIdentityMismatchFailsAsMissingAndExtra proves a
// live edge whose identity property VALUE does not match the expected-set
// entry is a real mismatch (missing + extra), not silently absorbed —
// exercising the same Key() the fixture side builds.
//
// The two assertions on the exact key strings, not merely that an error
// occurred, are load-bearing: the missing key is the fixture's own identity
// and would report regardless of whether the live side projects identity at
// all, but the extra key MUST carry the LIVE edge's full, length-prefixed
// identity suffix (pattern=*.go, source_path=CODEOWNERS). If the live
// identity projection were silently disabled, the live edge would key bare
// (DECLARES_CODEOWNER|repo-1|team-a, the raw "|"-joined shape the
// byte-identical no-Identity path still uses) instead — still a mismatch,
// so a bare "an error happened" check would stay green, but the extra-key
// assertion below would catch it, because the bare, raw-joined key cannot
// contain the length-prefixed identity suffix it asserts for.
func TestAssertMaterializedEdgesIdentityMismatchFailsAsMissingAndExtra(t *testing.T) {
	t.Parallel()

	types, err := ifa.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	identity := codeownersIdentityForTest(t)

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		codeownersEdge("repo-1", "team-a", "*.go", "CODEOWNERS"),
	}}
	expected := []ifa.ExpectedEdge{
		// Names the same endpoints but a different pattern than the graph has.
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a", Identity: map[string]string{"pattern": "*.py", "source_path": "CODEOWNERS"}},
	}

	err = assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, identity, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(identity value mismatch) = nil, want a missing/extra failure")
	}
	// Readable "TYPE|source|target|k=v|k=v" labels, not Key()'s netstring
	// comparison encoding: an operator reading this report at 3 AM must be
	// able to see the pattern/source_path values directly, not
	// "18:DECLARES_CODEOWNER6:repo-1...". expectedEdgeLabel renders the
	// readable form; Key() stays the injective comparison key underneath.
	const wantMissing = "DECLARES_CODEOWNER|repo-1|team-a|pattern=*.py|source_path=CODEOWNERS"
	const wantExtra = "DECLARES_CODEOWNER|repo-1|team-a|pattern=*.go|source_path=CODEOWNERS"
	if !strings.Contains(err.Error(), wantMissing) {
		t.Errorf("error %q does not name the missing fixture edge %q in readable form", err, wantMissing)
	}
	if !strings.Contains(err.Error(), wantExtra) {
		t.Errorf("error %q does not name the extra live edge %q in readable form — a disabled identity projection would key this edge bare instead", err, wantExtra)
	}
	if strings.Contains(err.Error(), ":DECLARES_CODEOWNER") {
		t.Errorf("error %q leaks Key()'s netstring encoding into the operator-facing report", err)
	}
}

// TestAssertMaterializedEdgesIdentityErrorOnMissingProperty proves a live
// edge of a type with a declared identity, but whose graph relationship is
// missing (or carries a non-string) declared property, is reported LOUD as an
// identity defect — never silently keyed with an empty "" value, which would
// let it either vanish or masquerade as a different edge.
func TestAssertMaterializedEdgesIdentityErrorOnMissingProperty(t *testing.T) {
	t.Parallel()

	types, err := ifa.MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges): %v", err)
	}
	identity := codeownersIdentityForTest(t)

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-1", TargetEntityID: "team-a", Identity: map[string]string{"pattern": "*.go", "source_path": "CODEOWNERS"}},
	}

	for _, tc := range []struct {
		name  string
		props map[string]any
	}{
		{"missing source_path entirely", map[string]any{"pattern": "*.go"}},
		{"non-string source_path", map[string]any{"pattern": "*.go", "source_path": 42}},
		{"blank source_path", map[string]any{"pattern": "*.go", "source_path": ""}},
		{"whitespace-only source_path", map[string]any{"pattern": "*.go", "source_path": "   "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := fakeEdgeReader{edges: []graphdump.Edge{{
				Type:      "DECLARES_CODEOWNER",
				FromProps: map[string]any{"id": "repo-1"},
				ToProps:   map[string]any{"uid": "team-a"},
				Props:     tc.props,
			}}}
			err := assertMaterializedEdges(context.Background(), graph, "codeowners_ownership_edges", types, nil, identity, expected)
			if err == nil {
				t.Fatal("an edge with a missing/non-string/blank declared identity property was accepted")
			}
			if !strings.Contains(err.Error(), "identity defects") {
				t.Errorf("error %q does not report an identity defect", err)
			}
			if !strings.Contains(err.Error(), "source_path") {
				t.Errorf("error %q does not name the offending property", err)
			}
		})
	}
}
