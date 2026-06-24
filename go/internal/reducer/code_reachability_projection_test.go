// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
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

// BenchmarkBuildCodeReachabilityRows records the bounded-traversal cost over a
// large synthetic corpus: a fan-out graph with depth and visited bounds applied.
// Run: go test ./internal/reducer -run='^$' -bench=BenchmarkBuildCodeReachabilityRows -benchmem
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
