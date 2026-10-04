// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeintel

import (
	"fmt"
	"testing"
)

// BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff measures the traversal
// when the depth bound stops the walk with a large frontier: a 4-ary tree of
// 50,000 nodes walked to MaxDepth 6, so roughly four thousand nodes sit at the
// cutoff and every one of them has unvisited children. It exercises the
// frontier scan that decides whether the snapshot is truncated by depth, which
// the 12-deep BenchmarkBuildCodeReachabilityRows corpus never reaches.
// Run: go test ./internal/reducer/codeintel -run='^$' -bench=BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff -benchmem -count=10
func BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff(b *testing.B) {
	const (
		nodes    = 50000
		fanOut   = 4
		maxDepth = 6
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
