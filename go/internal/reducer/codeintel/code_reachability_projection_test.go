// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeintel

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestBuildCodeReachabilityRowsWithStatsTruncatesAtMaxVisited(t *testing.T) {
	input := CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges: []CodeReachabilityEdge{
			{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:root", TargetEntityID: "entity:b", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:root", TargetEntityID: "entity:c", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		},
		MaxDepth:   5,
		MaxVisited: 2,
	}

	rows, stats := BuildCodeReachabilityRowsWithStats(input)
	if !stats.Truncated {
		t.Fatalf("stats.Truncated = false, want true at MaxVisited=2 with 4 reachable entities")
	}
	if got, want := stats.Visited, 2; got != want {
		t.Fatalf("stats.Visited = %d, want %d", got, want)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows = %d, want %d (root + one target under the bound): %#v", got, want, rows)
	}
}

func TestBuildCodeReachabilityRowsWithStatsReportsFullSetWhenUnbounded(t *testing.T) {
	input := CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges: []CodeReachabilityEdge{
			{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:root", TargetEntityID: "entity:b", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		},
		MaxDepth:   5,
		MaxVisited: 100,
	}

	rows, stats := BuildCodeReachabilityRowsWithStats(input)
	if stats.Truncated {
		t.Fatalf("stats.Truncated = true, want false under generous bound")
	}
	if got, want := stats.Visited, 3; got != want {
		t.Fatalf("stats.Visited = %d, want %d (root + 2 targets)", got, want)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
}

// codeReachabilityChain builds root -> n1 -> ... -> n<length> CALLS edges so a
// test can place the last node exactly at a chosen depth.
func codeReachabilityChain(length int) []CodeReachabilityEdge {
	edges := make([]CodeReachabilityEdge, 0, length)
	prev := "entity:root"
	for i := 1; i <= length; i++ {
		next := fmt.Sprintf("entity:n%d", i)
		edges = append(edges, CodeReachabilityEdge{
			SourceEntityID: prev, TargetEntityID: next, RelationshipType: "CALLS", ResolutionMethod: "scip",
		})
		prev = next
	}
	return edges
}

func TestBuildCodeReachabilityRowsWithStatsDepthCutoffWithoutFrontierIsComplete(t *testing.T) {
	// The last node sits exactly at MaxDepth and has no outgoing edge, so the
	// reachable set is fully enumerated.
	rows, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots:    []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges:    codeReachabilityChain(3),
		MaxDepth: 3,
	})
	if stats.Truncated {
		t.Fatalf("stats.Truncated = true (reason %q), want false: nothing lies beyond depth 3", stats.TruncationReason)
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
}

func TestBuildCodeReachabilityRowsWithStatsDepthCutoffBackEdgeToVisitedIsComplete(t *testing.T) {
	// A node at MaxDepth whose only outgoing edge targets an already visited
	// entity leaves nothing unseen, so the snapshot is still complete.
	edges := append(codeReachabilityChain(3), CodeReachabilityEdge{
		SourceEntityID: "entity:n3", TargetEntityID: "entity:root", RelationshipType: "CALLS", ResolutionMethod: "scip",
	})
	_, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots:    []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges:    edges,
		MaxDepth: 3,
	})
	if stats.Truncated {
		t.Fatalf("stats.Truncated = true (reason %q), want false: the only edge past the cutoff returns to a visited node", stats.TruncationReason)
	}
}

func TestBuildCodeReachabilityRowsWithStatsDepthCutoffWithFrontierIsTruncated(t *testing.T) {
	// n4 is reachable only at depth 4, past MaxDepth=3, and is silently dropped.
	rows, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots:    []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges:    codeReachabilityChain(4),
		MaxDepth: 3,
	})
	if !stats.Truncated {
		t.Fatalf("stats.Truncated = false, want true: entity:n4 lies beyond MaxDepth=3")
	}
	if got, want := stats.TruncationReason, CodeReachabilityTruncationMaxDepth; got != want {
		t.Fatalf("stats.TruncationReason = %q, want %q", got, want)
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("rows = %d, want %d (root..n3)", got, want)
	}
}

func TestBuildCodeReachabilityRowsWithStatsDefaultDepthTenFrontierIsTruncated(t *testing.T) {
	// Default MaxDepth is 10: an 11-hop chain drops its last node.
	_, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots: []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges: codeReachabilityChain(11),
	})
	if !stats.Truncated || stats.TruncationReason != CodeReachabilityTruncationMaxDepth {
		t.Fatalf("stats = %#v, want Truncated with reason %q", stats, CodeReachabilityTruncationMaxDepth)
	}
	_, stats = BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots: []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges: codeReachabilityChain(10),
	})
	if stats.Truncated {
		t.Fatalf("stats.Truncated = true, want false for a 10-hop chain at the default depth")
	}
}

func TestBuildCodeReachabilityRowsWithStatsZeroRootsIsTruncated(t *testing.T) {
	// A snapshot with no roots cannot prove any entity unreachable.
	for name, roots := range map[string][]CodeReachabilityRoot{
		"nil":        nil,
		"blank-only": {{EntityID: "  "}},
	} {
		t.Run(name, func(t *testing.T) {
			rows, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
				Roots: roots,
				Edges: codeReachabilityChain(2),
			})
			if len(rows) != 0 {
				t.Fatalf("rows = %#v, want none", rows)
			}
			if !stats.Truncated {
				t.Fatalf("stats.Truncated = false, want true for a zero-root snapshot")
			}
			if got, want := stats.TruncationReason, CodeReachabilityTruncationNoRoots; got != want {
				t.Fatalf("stats.TruncationReason = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildCodeReachabilityRowsWithStatsMaxVisitedReason(t *testing.T) {
	_, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots:      []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges:      codeReachabilityChain(4),
		MaxDepth:   10,
		MaxVisited: 2,
	})
	if got, want := stats.TruncationReason, CodeReachabilityTruncationMaxVisited; !stats.Truncated || got != want {
		t.Fatalf("stats = %#v, want Truncated with reason %q", stats, want)
	}
}

func TestBuildCodeReachabilityRowsWithStatsCompleteHasNoReason(t *testing.T) {
	_, stats := BuildCodeReachabilityRowsWithStats(CodeReachabilityProjectionInput{
		Roots:    []CodeReachabilityRoot{{EntityID: "entity:root"}},
		Edges:    codeReachabilityChain(2),
		MaxDepth: 5,
	})
	if stats.Truncated || stats.TruncationReason != "" {
		t.Fatalf("stats = %#v, want complete with empty reason", stats)
	}
}

// BenchmarkBuildCodeReachabilityRows records the bounded-traversal cost over a
// large synthetic corpus: a fan-out graph with depth and visited bounds applied.
// Run: go test ./internal/reducer/codeintel -run='^$' -bench=BenchmarkBuildCodeReachabilityRows -benchmem
func BenchmarkBuildCodeReachabilityRows(b *testing.B) {
	const (
		nodes    = 50000
		fanOut   = 4
		maxDepth = 12
	)
	edges := make([]CodeReachabilityEdge, 0, nodes*fanOut)
	for i := 0; i < nodes; i++ {
		for j := 1; j <= fanOut; j++ {
			target := i*fanOut + j
			if target >= nodes {
				break
			}
			edges = append(edges, CodeReachabilityEdge{
				SourceEntityID:   fmt.Sprintf("entity:%d", i),
				TargetEntityID:   fmt.Sprintf("entity:%d", target),
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			})
		}
	}
	input := CodeReachabilityProjectionInput{
		ScopeID:      "scope-bench",
		GenerationID: "generation-bench",
		RepositoryID: "repo-bench",
		Roots:        []CodeReachabilityRoot{{EntityID: "entity:0", RootKinds: []string{"go.main_function"}}},
		Edges:        edges,
		MaxDepth:     maxDepth,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, stats := BuildCodeReachabilityRowsWithStats(input)
		if len(rows) == 0 || stats.Visited == 0 {
			b.Fatalf("benchmark produced empty reachable set")
		}
	}
}

func TestBuildCodeReachabilityRowsComputesTransitiveReachableSet(t *testing.T) {
	rows := BuildCodeReachabilityRows(CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:  "entity:root",
			RootKinds: []string{"go.main_function"},
		}},
		Edges: []CodeReachabilityEdge{
			{
				SourceEntityID:   "entity:root",
				TargetEntityID:   "entity:service",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			},
			{
				SourceEntityID:   "entity:service",
				TargetEntityID:   "entity:helper",
				RelationshipType: "REFERENCES",
				ResolutionMethod: "repo_unique_name",
			},
			{
				SourceEntityID:   "entity:helper",
				TargetEntityID:   "entity:too-deep",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			},
		},
		MaxDepth: 2,
	})

	byEntity := make(map[string]CodeReachabilityRow)
	for _, row := range rows {
		byEntity[row.EntityID] = row
	}

	if _, ok := byEntity["entity:too-deep"]; ok {
		t.Fatalf("unexpected row past max depth: %#v", rows)
	}
	if got, want := byEntity["entity:root"].Depth, 0; got != want {
		t.Fatalf("root depth = %d, want %d", got, want)
	}
	if got, want := byEntity["entity:service"].Depth, 1; got != want {
		t.Fatalf("service depth = %d, want %d", got, want)
	}
	helper := byEntity["entity:helper"]
	if got, want := helper.Depth, 2; got != want {
		t.Fatalf("helper depth = %d, want %d", got, want)
	}
	if got, want := helper.State, CodeReachabilityStateAmbiguous; got != want {
		t.Fatalf("helper state = %q, want %q", got, want)
	}
	if got, want := helper.MinResolutionMethod, "repo_unique_name"; got != want {
		t.Fatalf("helper method = %q, want %q", got, want)
	}
}

func TestBuildCodeReachabilityRowsHandlesCyclesOnce(t *testing.T) {
	rows := BuildCodeReachabilityRows(CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:  "entity:root",
			RootKinds: []string{"go.main_function"},
		}},
		Edges: []CodeReachabilityEdge{
			{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:a", TargetEntityID: "entity:b", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:b", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:b", TargetEntityID: "entity:target", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		},
		MaxDepth:   10,
		MaxVisited: 10,
	})

	byEntity := make(map[string]CodeReachabilityRow)
	for _, row := range rows {
		if _, ok := byEntity[row.EntityID]; ok {
			t.Fatalf("duplicate reachable row for %q: %#v", row.EntityID, rows)
		}
		byEntity[row.EntityID] = row
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("rows = %d, want %d: %#v", got, want, rows)
	}
	if got, want := byEntity["entity:target"].Depth, 3; got != want {
		t.Fatalf("target depth = %d, want %d", got, want)
	}
}

func TestBuildCodeReachabilityRowsDeltaScopesAffectedSlice(t *testing.T) {
	rows := BuildCodeReachabilityRows(CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:  "entity:root",
			RootKinds: []string{"go.main_function"},
		}},
		Edges: []CodeReachabilityEdge{
			{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:a", TargetEntityID: "entity:b", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:root", TargetEntityID: "entity:c", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		},
		AffectedEntityIDs: []string{"entity:a"},
		MaxDepth:          5,
	})

	if got, want := len(rows), 2; got != want {
		t.Fatalf("delta rows = %d, want %d: %#v", got, want, rows)
	}
	gotEntities := map[string]bool{}
	for _, row := range rows {
		gotEntities[row.EntityID] = true
	}
	for _, want := range []string{"entity:a", "entity:b"} {
		if !gotEntities[want] {
			t.Fatalf("delta rows missing %s: %#v", want, rows)
		}
	}
	if gotEntities["entity:root"] || gotEntities["entity:c"] {
		t.Fatalf("delta rows included unaffected entities: %#v", rows)
	}
}

// TestBuildCodeReachabilityRowsEmitsOneRowPerEntityPerSnapshot pins the writer
// invariant the cross-repo dead-code evidence page's column fetch relies on
// (#7249): one snapshot carries at most one row per entity_id, however many
// roots reach it and by however many paths. The traversal is multi-source, so
// an entity reached from several roots keeps only its strongest shortest path.
// The page's fetch identifies a row by the primary key, which already yields at
// most one row; this invariant bounds the index entries the fetch reads when the
// planner serves it from an index whose key lacks root_entity_id. A build that
// kept one row per (root, entity) would put a busy entity's whole
// per-repository fan-in under every fetch.
func TestBuildCodeReachabilityRowsEmitsOneRowPerEntityPerSnapshot(t *testing.T) {
	const roots = 20
	input := CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		MaxDepth:     10,
	}
	for i := 0; i < roots; i++ {
		root := fmt.Sprintf("entity:root-%02d", i)
		input.Roots = append(input.Roots, CodeReachabilityRoot{EntityID: root, RootKinds: []string{"go.main_function"}})
		// Every root reaches the shared helper directly AND through its own
		// intermediate, and half of them reach it more strongly than the rest.
		method := "scip"
		if i%2 == 1 {
			method = "repo_unique_name"
		}
		middle := fmt.Sprintf("entity:middle-%02d", i)
		input.Edges = append(input.Edges,
			CodeReachabilityEdge{SourceEntityID: root, TargetEntityID: "entity:shared", RelationshipType: "CALLS", ResolutionMethod: method},
			CodeReachabilityEdge{SourceEntityID: root, TargetEntityID: middle, RelationshipType: "CALLS", ResolutionMethod: "scip"},
			CodeReachabilityEdge{SourceEntityID: middle, TargetEntityID: "entity:shared", RelationshipType: "REFERENCES", ResolutionMethod: "scip"},
			CodeReachabilityEdge{SourceEntityID: middle, TargetEntityID: "entity:leaf", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		)
	}

	rows := BuildCodeReachabilityRows(input)
	perEntity := make(map[string]int, len(rows))
	for _, row := range rows {
		perEntity[row.EntityID]++
		if row.ScopeID != "scope-1" || row.GenerationID != "generation-1" || row.RepositoryID != "repo-1" {
			t.Fatalf("row left its snapshot: %#v", row)
		}
	}
	for entityID, count := range perEntity {
		if count != 1 {
			t.Fatalf("%s has %d rows in one snapshot, want 1; the evidence page's per-row fetch would read them all", entityID, count)
		}
	}
	// Every root, its intermediate, the shared helper and the leaf: one each.
	if got, want := len(rows), 2*roots+2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	for _, row := range rows {
		if row.EntityID == "entity:shared" && (row.Depth != 1 || row.Confidence != codeprovenance.Confidence("scip")) {
			t.Fatalf("entity:shared kept depth %d confidence %v, want its strongest shortest path (depth 1, scip)", row.Depth, row.Confidence)
		}
	}
}
