// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type anchorFallbackGraphReader struct {
	indexed     []map[string]any
	complete    []map[string]any
	fallbackErr error
	calls       []string
}

func (f *anchorFallbackGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (f *anchorFallbackGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.calls = append(f.calls, cypher)
	if strings.Contains(cypher, "s.uid IS NOT NULL") {
		return f.indexed, nil
	}
	if f.fallbackErr != nil {
		return nil, f.fallbackErr
	}
	return f.complete, nil
}

func edgeRow(id string) map[string]any {
	return map[string]any{"source_id": id, "target_id": "target-" + id}
}

func TestRelationshipEdgesRetainsMissingAnchorRowsAndTruncation(t *testing.T) {
	t.Parallel()
	entry := relationshipVerbByName["CALLS"]
	for _, tc := range []struct {
		name          string
		indexed       []map[string]any
		complete      []map[string]any
		wantIDs       []string
		wantTruncated bool
		wantCalls     int
	}{
		{"fast page", []map[string]any{edgeRow("a"), edgeRow("b"), edgeRow("c")}, nil, []string{"a", "b"}, true, 1},
		{"null is extra", []map[string]any{edgeRow("a"), edgeRow("b")}, []map[string]any{edgeRow("a"), edgeRow("b"), edgeRow("null")}, []string{"a", "b"}, true, 2},
		{"null enters page", []map[string]any{edgeRow("a")}, []map[string]any{edgeRow("a"), edgeRow("null")}, []string{"a", "null"}, false, 2},
		{"all null", nil, []map[string]any{edgeRow("null")}, []string{"null"}, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := &anchorFallbackGraphReader{indexed: tc.indexed, complete: tc.complete}
			h := &InfraHandler{Neo4j: graph}
			edges, truncated, err := h.relationshipEdges(context.Background(), entry, "", 2, querycontract.RepositoryAccessFilter{AllScopes: true})
			if err != nil {
				t.Fatal(err)
			}
			if truncated != tc.wantTruncated || len(edges) != len(tc.wantIDs) || len(graph.calls) != tc.wantCalls {
				t.Fatalf("edges=%v truncated=%t calls=%d", edges, truncated, len(graph.calls))
			}
			for i, id := range tc.wantIDs {
				if edges[i].SourceID != id {
					t.Fatalf("edge[%d]=%q want %q", i, edges[i].SourceID, id)
				}
			}
		})
	}
}

func TestRelationshipEdgesFallbackErrorPropagates(t *testing.T) {
	t.Parallel()
	want := errors.New("fallback failed")
	graph := &anchorFallbackGraphReader{fallbackErr: want}
	h := &InfraHandler{Neo4j: graph}
	_, _, err := h.relationshipEdges(context.Background(), relationshipVerbByName["CALLS"], "", 2, querycontract.RepositoryAccessFilter{AllScopes: true})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want %v", err, want)
	}
}

func TestRelationshipEdgesFilteredAndScopedUseOneCompleteRead(t *testing.T) {
	t.Parallel()
	entry := relationshipVerbByName["CALLS"]
	for _, tc := range []struct {
		name   string
		tool   string
		access querycontract.RepositoryAccessFilter
	}{
		{"filtered", "terraform", querycontract.RepositoryAccessFilter{AllScopes: true}},
		{"scoped", "", querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:allowed"}}},
		{"filtered and scoped", "terraform", querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:allowed"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := &anchorFallbackGraphReader{complete: []map[string]any{edgeRow("allowed-null")}}
			h := &InfraHandler{Neo4j: graph}
			edges, truncated, err := h.relationshipEdges(context.Background(), entry, tc.tool, 2, tc.access)
			if err != nil {
				t.Fatal(err)
			}
			if truncated || len(edges) != 1 || edges[0].SourceID != "allowed-null" || len(graph.calls) != 1 {
				t.Fatalf("edges=%v truncated=%t calls=%d", edges, truncated, len(graph.calls))
			}
			cypher := graph.calls[0]
			if strings.Contains(cypher, "s.uid IS NOT NULL") {
				t.Fatalf("sparse query used indexed probe: %s", cypher)
			}
			if tc.tool != "" && !strings.Contains(cypher, "r.source_tool = $source_tool") {
				t.Fatalf("source-tool filter missing: %s", cypher)
			}
			if tc.access.Scoped() && (!strings.Contains(cypher, "s.repo_id IN $allowed_repository_ids") || !strings.Contains(cypher, "t.repo_id IN $allowed_repository_ids")) {
				t.Fatalf("scope predicate missing: %s", cypher)
			}
		})
	}
}
