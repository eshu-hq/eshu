// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

func TestGraphSummaryHotEntitiesNeo4jUsesBoundedDegreePage(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		multi: func(cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "collect(DISTINCT [source,target])") {
				t.Fatalf("Neo4j hot-entity query does not deduplicate CALLS pairs: %q", cypher)
			}
			if got, want := params["edge_scan_limit"], codequery.CallGraphMetricsEdgeScanLimit+1; got != want {
				t.Fatalf("edge_scan_limit = %#v, want %#v", got, want)
			}
			if got, want := params["rank_limit"], 2; got != want {
				t.Fatalf("rank_limit = %#v, want %#v", got, want)
			}
			return []map[string]any{
				{"raw_edges": int64(3), "invalid_uid_edges": int64(0), "function_id": "a", "function_name": "alpha", "file_path": "a.go", "incoming": int64(2), "outgoing": int64(1), "total_degree": int64(3)},
				{"raw_edges": int64(3), "invalid_uid_edges": int64(0), "function_id": "b", "function_name": "beta", "file_path": "b.go", "incoming": int64(1), "outgoing": int64(1), "total_degree": int64(2)},
			}, nil
		},
	}
	handler := &InfraHandler{GraphBackend: GraphBackendNeo4j, Neo4j: reader}

	rows, truncated, err := handler.graphSummaryHotEntities(context.Background(), "repository:r", 1)
	if err != nil {
		t.Fatalf("graphSummaryHotEntities() error = %v", err)
	}
	if !truncated || len(rows) != 1 {
		t.Fatalf("page = (%d rows, truncated=%t), want (1, true)", len(rows), truncated)
	}
	if got := StringVal(rows[0], "function_id"); got != "a" {
		t.Fatalf("function_id = %q, want a", got)
	}
	if got := IntVal(rows[0], "total_degree"); got != 3 {
		t.Fatalf("total_degree = %d, want 3", got)
	}
}

func TestGraphSummaryHotEntitiesNeo4jFailsClosedOnRawEdgeOverflow(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		multi: func(_ string, _ map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"raw_edges": int64(codequery.CallGraphMetricsEdgeScanLimit + 1)}}, nil
		},
	}
	handler := &InfraHandler{GraphBackend: GraphBackendNeo4j, Neo4j: reader}
	_, _, err := handler.graphSummaryHotEntities(context.Background(), "repository:r", 10)
	if !errors.Is(err, errGraphSummaryScopeTooBroad) {
		t.Fatalf("graphSummaryHotEntities() error = %v, want errGraphSummaryScopeTooBroad", err)
	}
}

func TestGraphSummaryHotEntitiesNeo4jFallsBackForMissingUID(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := &graphSummaryRecordingReader{
		multi: func(cypher string, _ map[string]any) ([]map[string]any, error) {
			calls++
			if calls == 1 {
				return []map[string]any{{"raw_edges": int64(1), "invalid_uid_edges": int64(1)}}, nil
			}
			if !strings.Contains(cypher, "RETURN source.uid AS source_uid") {
				t.Fatalf("fallback query = %q, want the exact raw edge pass", cypher)
			}
			return []map[string]any{callGraphMetricEdgeRow("a", "a.go", "go", "alpha", 1, "b", "b.go", "go", "beta", 2)}, nil
		},
	}
	handler := &InfraHandler{GraphBackend: GraphBackendNeo4j, Neo4j: reader}
	rows, _, err := handler.graphSummaryHotEntities(context.Background(), "repository:r", 10)
	if err != nil {
		t.Fatalf("graphSummaryHotEntities() error = %v", err)
	}
	if calls != 2 || len(rows) != 2 {
		t.Fatalf("fallback = (%d calls, %d rows), want (2, 2)", calls, len(rows))
	}
}
