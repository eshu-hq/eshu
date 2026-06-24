// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// TestRelationshipStoryRanksMostConnectedFirst proves that, within the resolved
// bounded result set, neighbors that appear across more edges (here via a
// multi-type filter) are ranked ahead of less-connected neighbors, so a small
// limit or token_budget keeps the most useful rows.
func TestRelationshipStoryRanksMostConnectedFirst(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, ":CALLS"):
					return []map[string]any{
						{"direction": "incoming", "type": "CALLS", "source_id": "shared", "source_name": "shared", "target_id": "fn-t", "target_name": "target"},
						{"direction": "incoming", "type": "CALLS", "source_id": "callsonly", "source_name": "callsonly", "target_id": "fn-t", "target_name": "target"},
					}, nil
				case strings.Contains(cypher, ":IMPORTS"):
					return []map[string]any{
						{"direction": "incoming", "type": "IMPORTS", "source_id": "shared", "source_name": "shared", "target_id": "fn-t", "target_name": "target"},
					}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
					return nil, nil
				}
			},
		},
	}

	resp := postRelationshipStory(t, handler,
		`{"entity_id":"fn-t","relationship_types":["CALLS","IMPORTS"],"direction":"incoming","limit":10}`)

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if got, want := len(relationships), 3; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	first := relationships[0].(map[string]any)
	second := relationships[1].(map[string]any)
	third := relationships[2].(map[string]any)
	if got := StringVal(first, "source_id"); got != "shared" {
		t.Fatalf("relationships[0].source_id = %q, want shared", got)
	}
	if got := StringVal(second, "source_id"); got != "shared" {
		t.Fatalf("relationships[1].source_id = %q, want shared", got)
	}
	if got := StringVal(third, "source_id"); got != "callsonly" {
		t.Fatalf("relationships[2].source_id = %q, want callsonly", got)
	}
	if got, want := intFromJSON(first["centrality"]), 2; got != want {
		t.Fatalf("relationships[0].centrality = %#v, want %d", first["centrality"], want)
	}
	if got, want := intFromJSON(third["centrality"]), 1; got != want {
		t.Fatalf("relationships[2].centrality = %#v, want %d", third["centrality"], want)
	}
	coverage := resp["coverage"].(map[string]any)
	if got, want := coverage["ranked_by"], "bounded_centrality"; got != want {
		t.Fatalf("coverage.ranked_by = %#v, want %#v", got, want)
	}
}

// TestRelationshipStoryCentralityStableTieBreak proves that when every neighbor
// is equally connected, ranking preserves the deterministic incoming order
// produced by the bounded query (name then id), not an arbitrary one.
func TestRelationshipStoryCentralityStableTieBreak(t *testing.T) {
	t.Parallel()

	resp := postRelationshipStory(t, threeIncomingCallersHandler(),
		`{"entity_id":"fn-t","relationship_type":"CALLS","direction":"incoming","limit":10}`)

	relationships := resp["relationships"].([]any)
	if got, want := len(relationships), 3; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	wantOrder := []string{"fn-a", "fn-b", "fn-c"}
	for index, want := range wantOrder {
		row := relationships[index].(map[string]any)
		if got := StringVal(row, "source_id"); got != want {
			t.Fatalf("relationships[%d].source_id = %q, want %q (tie-break must preserve input order)", index, got, want)
		}
		if got, want := intFromJSON(row["centrality"]), 1; got != want {
			t.Fatalf("relationships[%d].centrality = %#v, want %d", index, row["centrality"], want)
		}
	}
}

// TestRelationshipStoryCentralityRanksBeforeLimit proves the most-connected
// neighbors survive the count limit, not merely the first rows the graph
// returned.
func TestRelationshipStoryCentralityRanksBeforeLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, ":CALLS"):
					return []map[string]any{
						{"direction": "incoming", "type": "CALLS", "source_id": "low", "source_name": "low", "target_id": "fn-t", "target_name": "target"},
					}, nil
				case strings.Contains(cypher, ":IMPORTS"):
					return []map[string]any{
						{"direction": "incoming", "type": "IMPORTS", "source_id": "hub", "source_name": "hub", "target_id": "fn-t", "target_name": "target"},
						{"direction": "incoming", "type": "IMPORTS", "source_id": "hub", "source_name": "hub", "target_id": "fn-t", "target_name": "target"},
					}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
					return nil, nil
				}
			},
		},
	}

	// limit 1 keeps only the most-connected neighbor.
	resp := postRelationshipStory(t, handler,
		`{"entity_id":"fn-t","relationship_types":["CALLS","IMPORTS"],"direction":"incoming","limit":1}`)

	relationships := resp["relationships"].([]any)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("len(relationships) = %d, want %d", got, want)
	}
	if got := StringVal(relationships[0].(map[string]any), "source_id"); got != "hub" {
		t.Fatalf("relationships[0].source_id = %q, want hub (highest centrality survives the limit)", got)
	}
}
