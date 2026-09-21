// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"
)

// diamondExpand is the #6834 diamond: a-b-c-d-e, a-b-e, a-x-e, a-y-e, with
// weakest-edge confidences per hop.
func diamondExpand() func(context.Context, string) ([]compareHop, error) {
	edges := map[string][]compareHop{
		"a": {{"b", "b", 0.9}, {"x", "x", 0.95}, {"y", "y", 0.75}},
		"b": {{"c", "c", 0.8}, {"e", "e", 0.5}},
		"c": {{"d", "d", 0.7}},
		"d": {{"e", "e", 0.6}},
		"x": {{"e", "e", 0.85}},
		"y": {{"e", "e", 0.65}},
	}
	return func(_ context.Context, id string) ([]compareHop, error) {
		return edges[id], nil
	}
}

// TestBFSComparePathsFindsAllSimplePaths pins the diamond contract: all
// four simple paths, shortest first, each with weakest-edge confidence.
func TestBFSComparePathsFindsAllSimplePaths(t *testing.T) {
	result, err := bfsComparePaths(context.Background(), "a", "e",
		compareEnds{startName: "a", endName: "e"}, 4, 5, CompareVisitBudget, diamondExpand())
	if err != nil {
		t.Fatalf("bfsComparePaths error = %v", err)
	}
	paths := result.Paths
	if result.Truncated {
		t.Errorf("truncated = true, want false (exhaustive under caps)")
	}
	if len(paths) != 4 {
		t.Fatalf("paths = %d, want 4: %+v", len(paths), paths)
	}
	if paths[0].Depth != 2 || paths[3].Depth != 4 {
		t.Errorf("depths = %d..%d, want shortest-first 2..4", paths[0].Depth, paths[3].Depth)
	}
	// a-x-e carries min(0.95, 0.85) = 0.85 and sorts before a-b-e at 0.5
	// only by length; confidence rides each path regardless of order.
	confidences := map[string]float64{}
	for _, path := range paths {
		key := ""
		for _, node := range path.Nodes {
			key += node.ID
		}
		confidences[key] = path.Confidence
	}
	if confidences["axe"] != 0.85 {
		t.Errorf("a-x-e confidence = %v, want 0.85", confidences["axe"])
	}
	if confidences["abcde"] != 0.6 {
		t.Errorf("a-b-c-d-e confidence = %v, want 0.6", confidences["abcde"])
	}
}

// TestBFSComparePathsCapsAtMaxPaths pins the K bound: the fifth path drops
// and truncated reports more available.
func TestBFSComparePathsCapsAtMaxPaths(t *testing.T) {
	result, err := bfsComparePaths(context.Background(), "a", "e",
		compareEnds{startName: "a", endName: "e"}, 4, 2, CompareVisitBudget, diamondExpand())
	if err != nil {
		t.Fatalf("bfsComparePaths error = %v", err)
	}
	if len(result.Paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(result.Paths))
	}
	if !result.Truncated {
		t.Errorf("truncated = false, want true (cap filled with more available)")
	}
}

// TestBFSComparePathsRejectsCycles pins simplicity: a Bohrium loop (b
// calls back to a) never emits a repeated node.
func TestBFSComparePathsRejectsCycles(t *testing.T) {
	expand := diamondExpand()
	looping := func(ctx context.Context, id string) ([]compareHop, error) {
		hops, err := expand(ctx, id)
		if err != nil {
			return nil, err
		}
		if id == "b" {
			hops = append(hops, compareHop{ID: "a", Name: "a", Confidence: 0.1})
		}
		return hops, nil
	}
	result, err := bfsComparePaths(context.Background(), "a", "e",
		compareEnds{startName: "a", endName: "e"}, 6, 20, CompareVisitBudget, looping)
	if err != nil {
		t.Fatalf("bfsComparePaths error = %v", err)
	}
	paths := result.Paths
	for _, path := range paths {
		seen := map[string]bool{}
		for _, node := range path.Nodes {
			if seen[node.ID] {
				t.Errorf("path repeats %q: %+v", node.ID, path.Nodes)
			}
			seen[node.ID] = true
		}
	}
	if len(paths) != 4 {
		t.Errorf("paths = %d, want 4 (loop adds no simple path)", len(paths))
	}
}

// TestBFSComparePathsNoPath pins the empty answer: disconnected entities
// report no paths without truncation.
func TestBFSComparePathsNoPath(t *testing.T) {
	result, err := bfsComparePaths(context.Background(), "a", "zzz",
		compareEnds{startName: "a", endName: "zzz"}, 4, 5, CompareVisitBudget, diamondExpand())
	if err != nil {
		t.Fatalf("bfsComparePaths error = %v", err)
	}
	if len(result.Paths) != 0 {
		t.Errorf("paths = %d, want 0", len(result.Paths))
	}
	if result.Truncated {
		t.Errorf("truncated = true, want false (exhaustive)")
	}
}

// TestBFSComparePathsSelfEndpoint pins the degenerate answer: an entity
// compared with itself returns the zero-length path, never a cycle hunt.
func TestBFSComparePathsSelfEndpoint(t *testing.T) {
	result, err := bfsComparePaths(context.Background(), "a", "a",
		compareEnds{startName: "a", endName: "a"}, 4, 5, CompareVisitBudget, diamondExpand())
	if err != nil {
		t.Fatalf("bfsComparePaths error = %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0].Depth != 0 {
		t.Errorf("paths = %+v, want one zero-length path", result.Paths)
	}
	if result.Truncated {
		t.Errorf("truncated = true, want false")
	}
}
