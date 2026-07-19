// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFetchWorkloadPlatformResultReportsSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "LIMIT $platform_edge_limit") {
			t.Fatalf("platform query is unbounded: %s", cypher)
		}
		if got, want := IntVal(params, "platform_edge_limit"), workloadPlatformEdgeLimit+1; got != want {
			t.Fatalf("platform_edge_limit = %d, want %d", got, want)
		}
		rows := make([]map[string]any, 0, workloadPlatformEdgeLimit+1)
		for index := range workloadPlatformEdgeLimit + 1 {
			rows = append(rows, map[string]any{
				"instance_id": "instance:orders:prod", "platform_id": fmt.Sprintf("platform:%04d", index),
			})
		}
		return rows, nil
	}}
	result, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadPlatformResult(
		t.Context(), []map[string]any{{"instance_id": "instance:orders:prod"}},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadPlatformResult() error = %v", err)
	}
	if got, want := len(result.rows), workloadPlatformEdgeLimit; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want lower-bound truncation", result.limits)
	}
}

func TestFetchWorkloadRuntimeTopologyReturnsObservedIdentityEdges(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "MATCH (repo:Repository)-[defines:DEFINES]->(w:Workload)<-[instanceOf:INSTANCE_OF]-(i:WorkloadInstance)") {
			t.Fatalf("runtime topology cypher = %q, want one exact DEFINES/INSTANCE_OF clause", cypher)
		}
		if got, want := IntVal(params, "instance_limit"), contextStoryItemLimit+1; got != want {
			t.Fatalf("instance_limit = %d, want %d", got, want)
		}
		return []map[string]any{{
			"repo_id": "repository:orders", "repo_name": "orders", "workload_id": "workload:orders",
			"instance_id": "workload-instance:orders:prod", "environment": "prod",
			"defines_edge":  map[string]any{"confidence": 0.99, "source_tool": "dockerfile", "source_fact_id": "fact-defines"},
			"instance_edge": map[string]any{"confidence": 0.94, "evidence_source": "helm", "source_fact_id": "fact-instance"},
		}}, nil
	}}

	result, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	if got, want := len(result.instances), 1; got != want {
		t.Fatalf("instances = %d, want %d", got, want)
	}
	assertExactTopologyEdge(t, result.topologyEdges, "DEFINES", "repository:orders", "workload:orders", "fact-defines")
	assertExactTopologyEdge(t, result.topologyEdges, "INSTANCE_OF", "workload-instance:orders:prod", "workload:orders", "fact-instance")
	if BoolVal(result.limits, "truncated") {
		t.Fatalf("limits = %#v, want complete", result.limits)
	}
}

func TestFetchWorkloadRuntimeTopologyScopesDefiningRepositoriesToCallerGrants(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "repo.id IN $allowed_repository_ids") ||
			!strings.Contains(cypher, "repo.id IN $allowed_scope_ids") ||
			!strings.Contains(cypher, "repo.id = $repo_id") {
			return []map[string]any{
				{
					"repo_id": "repository:allowed", "repo_name": "allowed", "workload_id": "workload:orders",
					"instance_id": "workload-instance:orders:prod", "environment": "prod",
				},
				{
					"repo_id": "repository:allowed", "repo_name": "allowed", "workload_id": "workload:orders",
					"instance_id": "workload-instance:orders:secret", "environment": "secret",
					"instance_edge": map[string]any{"source_fact_id": "fact-secret"},
				},
			}, nil
		}
		if got := StringSliceVal(params, "allowed_repository_ids"); len(got) != 1 || got[0] != "repository:allowed" {
			t.Fatalf("allowed_repository_ids = %#v, want only repository:allowed", got)
		}
		if got, want := StringVal(params, "repo_id"), "repository:allowed"; got != want {
			t.Fatalf("repo_id = %q, want selected %q", got, want)
		}
		if strings.Contains(cypher, "i.repo_id") {
			t.Fatalf("runtime topology must not invent WorkloadInstance repository ownership: %s", cypher)
		}
		return []map[string]any{{
			"repo_id": "repository:allowed", "repo_name": "allowed", "workload_id": "workload:orders",
			"instance_id": "workload-instance:orders:prod", "environment": "prod",
		}}, nil
	}}
	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:allowed"},
	})

	result, err := fetchWorkloadRuntimeTopology(
		ctx, reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:allowed",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	for _, edge := range result.topologyEdges {
		if got := StringVal(edge, "source_id"); got == "repository:secret" || got == "workload-instance:orders:secret" {
			t.Fatalf("topology edge leaked unauthorized repository or instance: %#v", edge)
		}
	}
}

func TestFetchWorkloadRuntimeTopologyReportsInstanceSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		rows := make([]map[string]any, 0, contextStoryItemLimit+1)
		for index := range contextStoryItemLimit + 1 {
			rows = append(rows, map[string]any{
				"repo_id": "repository:orders", "workload_id": "workload:orders",
				"instance_id": "instance:" + string(rune('A'+index)),
			})
		}
		return rows, nil
	}}

	result, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	if got, want := len(result.instances), contextStoryItemLimit; got != want {
		t.Fatalf("instances = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want lower-bound truncation", result.limits)
	}
}

func assertExactTopologyEdge(t *testing.T, edges []map[string]any, relationshipType, sourceID, targetID, sourceFactID string) {
	t.Helper()
	for _, edge := range edges {
		if StringVal(edge, "relationship_type") != relationshipType ||
			StringVal(edge, "source_id") != sourceID || StringVal(edge, "target_id") != targetID {
			continue
		}
		if got := StringVal(mapValue(edge, "properties"), "source_fact_id"); got != sourceFactID {
			t.Fatalf("%s properties.source_fact_id = %q, want %q", relationshipType, got, sourceFactID)
		}
		return
	}
	t.Fatalf("edges = %#v, want %s %s -> %s", edges, relationshipType, sourceID, targetID)
}
