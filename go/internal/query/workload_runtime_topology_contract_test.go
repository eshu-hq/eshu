// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestFetchWorkloadRuntimeTopologyStartsFromWorkloadInstanceTraversal(t *testing.T) {
	t.Parallel()

	var capturedCypher string
	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		capturedCypher = cypher
		return []map[string]any{}, nil
	}}

	_, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	const workloadFirstTraversal = "MATCH (i:WorkloadInstance)-[instanceOf:INSTANCE_OF]->(w:Workload)<-[defines:DEFINES]-(repo:Repository)"
	if !strings.Contains(capturedCypher, workloadFirstTraversal) {
		t.Fatalf("runtime topology cypher = %q, want WorkloadInstance-first traversal %q", capturedCypher, workloadFirstTraversal)
	}
	if strings.Contains(capturedCypher, "MATCH (repo:Repository)-[defines:DEFINES]->") {
		t.Fatalf("runtime topology cypher = %q, must not start from Repository", capturedCypher)
	}
	if !strings.Contains(capturedCypher, "WHERE i.workload_id = $workload_id") {
		t.Fatalf("runtime topology cypher = %q, want workload_id predicate", capturedCypher)
	}
}

func TestFetchWorkloadDeploymentTopologyReturnsStructuredEmptyLimits(t *testing.T) {
	t.Parallel()

	runtimeQueryCalls := 0
	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		runtimeQueryCalls++
		if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[instanceOf:INSTANCE_OF]->(w:Workload)<-[defines:DEFINES]-(repo:Repository)") {
			t.Fatalf("unexpected graph query for empty runtime topology: %s", cypher)
		}
		return []map[string]any{}, nil
	}}

	result, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadDeploymentTopology(
		t.Context(), "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders", false,
	)
	if err != nil {
		t.Fatalf("fetchWorkloadDeploymentTopology() error = %v", err)
	}
	if runtimeQueryCalls != 1 {
		t.Fatalf("runtime graph query calls = %d, want 1", runtimeQueryCalls)
	}
	assertEmptyTopologyLimits(
		t, result.instanceLimits, contextStoryItemLimit, contextStoryItemLimit+1,
		[]string{"environment", "instance_id"},
	)
	assertEmptyTopologyLimits(
		t, result.platformLimits, workloadPlatformEdgeLimit, workloadPlatformEdgeLimit+1,
		[]string{"instance_id", "platform_name", "platform_id"},
	)
	assertEmptyTopologyLimits(
		t, result.provisionedPlatformLimits, contextStoryItemLimit, contextStoryItemLimit+1,
		[]string{"platform_name", "platform_id", "source_repository_id", "target_repository_id"},
	)
}

func TestFetchWorkloadDeploymentTopologyOmitsUnownedRuntimeForScopedTokens(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		calls++
		return []map[string]any{{"instance_id": "workload-instance:orders:unowned"}}, nil
	}}
	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:allowed"},
	})

	result, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadDeploymentTopology(
		ctx, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:allowed", false,
	)
	if err != nil {
		t.Fatalf("fetchWorkloadDeploymentTopology() error = %v", err)
	}
	if len(result.instances) != 0 || len(result.topologyEdges) != 0 {
		t.Fatalf("topology = %#v, want no repository-unowned runtime evidence", result)
	}
	if len(result.instanceLimits) != 0 {
		t.Fatalf("instance limits = %#v, want completeness metadata withheld with runtime evidence", result.instanceLimits)
	}
	if calls != 0 {
		t.Fatalf("graph calls = %d, want zero for scoped token", calls)
	}
}

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
		t.Context(), "repository:orders", "workload:orders",
		[]map[string]any{{"instance_id": "instance:orders:prod"}},
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

func TestFetchWorkloadPlatformResultRejectsNonJSONRelationshipProperties(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"instance_id": "workload-instance:orders:prod", "platform_id": "platform:eks:prod",
			"platform_edges": []map[string]any{{"confidence": math.Inf(-1)}},
		}}, nil
	}}

	_, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadPlatformResult(
		t.Context(), "repository:orders", "workload:orders",
		[]map[string]any{{"instance_id": "workload-instance:orders:prod"}},
	)
	if err == nil || !strings.Contains(err.Error(), "marshal graph relationship properties") {
		t.Fatalf("fetchWorkloadPlatformResult() error = %v, want non-JSON relationship-property error", err)
	}
}

func TestFetchWorkloadRuntimeTopologyReturnsObservedIdentityEdges(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[instanceOf:INSTANCE_OF]->(w:Workload)<-[defines:DEFINES]-(repo:Repository)") {
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

func TestFetchWorkloadRuntimeTopologyRejectsNonJSONRelationshipProperties(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"repo_id": "repository:orders", "workload_id": "workload:orders",
			"instance_id":  "workload-instance:orders:prod",
			"defines_edge": map[string]any{"confidence": math.NaN()},
		}}, nil
	}}

	_, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders",
	)
	if err == nil || !strings.Contains(err.Error(), "marshal graph relationship properties") {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v, want non-JSON relationship-property error", err)
	}
}

func TestFetchWorkloadRuntimeTopologyRejectsNonFiniteInstanceConfidence(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"repo_id": "repository:orders", "workload_id": "workload:orders",
			"instance_id":                "workload-instance:orders:prod",
			"materialization_confidence": math.NaN(),
		}}, nil
	}}

	_, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders"},
		"repository:orders",
	)
	if err == nil || !strings.Contains(err.Error(), "materialization_confidence") {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v, want non-finite instance-confidence error", err)
	}
}

func TestFetchWorkloadRuntimeTopologyOmitsUnownedInstancesForScopedTokens(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		calls++
		return []map[string]any{{"instance_id": "workload-instance:orders:unowned"}}, nil
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
	if len(result.instances) != 0 || len(result.topologyEdges) != 0 {
		t.Fatalf("topology = %#v, want no repository-unowned runtime evidence", result)
	}
	if len(result.limits) != 0 {
		t.Fatalf("limits = %#v, want completeness metadata withheld with runtime evidence", result.limits)
	}
	if calls != 0 {
		t.Fatalf("graph calls = %d, want zero for scoped token", calls)
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

func assertEmptyTopologyLimits(
	t *testing.T,
	limits map[string]any,
	wantLimit int,
	wantQueryLimit int,
	wantOrdering []string,
) {
	t.Helper()
	if len(limits) == 0 {
		t.Fatal("limits are missing, want a structured empty collection contract")
	}
	for _, field := range []string{"returned_count", "observed_count"} {
		if got := IntVal(limits, field); got != 0 {
			t.Fatalf("limits.%s = %d, want 0; limits = %#v", field, got, limits)
		}
	}
	for _, field := range []string{"observed_count_is_lower_bound", "truncated"} {
		value, ok := limits[field].(bool)
		if !ok || value {
			t.Fatalf("limits.%s = %#v, want false; limits = %#v", field, limits[field], limits)
		}
	}
	if got := IntVal(limits, "limit"); got != wantLimit {
		t.Fatalf("limits.limit = %d, want %d; limits = %#v", got, wantLimit, limits)
	}
	if got := IntVal(limits, "query_sentinel_limit"); got != wantQueryLimit {
		t.Fatalf("limits.query_sentinel_limit = %d, want %d; limits = %#v", got, wantQueryLimit, limits)
	}
	if got := StringSliceVal(limits, "ordering"); strings.Join(got, "\x00") != strings.Join(wantOrdering, "\x00") {
		t.Fatalf("limits.ordering = %#v, want %#v", got, wantOrdering)
	}
}
