// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGraphSummaryHotEntitiesUsesExactBoundedCallEdgePass(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		multi: func(cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})") {
				t.Fatalf("hot-entity query = %q, want the NornicDB-safe single CALLS edge pass", cypher)
			}
			if got, want := params["repo_id"], "repository:r_dart"; got != want {
				t.Fatalf("repo_id = %#v, want %#v", got, want)
			}
			if got, want := params["edge_scan_limit"], callGraphMetricsEdgeScanLimit+1; got != want {
				t.Fatalf("edge_scan_limit = %#v, want %#v", got, want)
			}
			return []map[string]any{
				callGraphMetricEdgeRow("fn-ping", "calls.dart", "dart", "mutualPing", 48, "fn-pong", "calls.dart", "dart", "mutualPong", 52),
				callGraphMetricEdgeRow("fn-pong", "calls.dart", "dart", "mutualPong", 52, "fn-ping", "calls.dart", "dart", "mutualPing", 48),
			}, nil
		},
	}

	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}
	rows, truncated, err := handler.graphSummaryHotEntities(context.Background(), "repository:r_dart", 10)
	if err != nil {
		t.Fatalf("graphSummaryHotEntities() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("graphSummaryHotEntities() truncated = true, want false")
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	first := rows[0]
	if got, want := StringVal(first, "function_name"), "mutualPing"; got != want {
		t.Fatalf("function_name = %q, want %q", got, want)
	}
	for field, want := range map[string]int{
		"incoming_calls": 1,
		"outgoing_calls": 1,
		"total_degree":   2,
	} {
		if got := IntVal(first, field); got != want {
			t.Fatalf("%s = %d, want %d", field, got, want)
		}
	}
}

func TestGraphSummaryHotEntitiesFailsClosedAtEdgeScanSentinel(t *testing.T) {
	t.Parallel()

	reader := &graphSummaryRecordingReader{
		multi: func(_ string, _ map[string]any) ([]map[string]any, error) {
			return make([]map[string]any, callGraphMetricsEdgeScanLimit+1), nil
		},
	}
	handler := &InfraHandler{Profile: ProfileProduction, Neo4j: reader}

	_, _, err := handler.graphSummaryHotEntities(context.Background(), "repository:r_large", 10)
	if !errors.Is(err, errGraphSummaryScopeTooBroad) {
		t.Fatalf("graphSummaryHotEntities() error = %v, want errGraphSummaryScopeTooBroad", err)
	}
}
