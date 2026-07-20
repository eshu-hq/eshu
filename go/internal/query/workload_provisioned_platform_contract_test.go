// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFetchProvisionedPlatformsReportsUniqueSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		rows := make([]map[string]any, 0, contextStoryItemLimit+1)
		for index := range contextStoryItemLimit + 1 {
			rows = append(rows, map[string]any{
				"platform_source_id": "repository:infra", "platform_dependency_target_id": "repository:orders",
				"platform_id": fmt.Sprintf("platform:%03d", index), "platform_name": fmt.Sprintf("platform-%03d", index),
			})
		}
		return rows, nil
	}}
	result, err := (&EntityHandler{Neo4j: reader}).fetchProvisionedPlatformResult(t.Context(), "repository:orders")
	if err != nil {
		t.Fatalf("fetchProvisionedPlatformResult() error = %v", err)
	}
	if got, want := len(result.rows), contextStoryItemLimit; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want lower-bound truncation", result.limits)
	}
}

func TestFetchProvisionedPlatformsKeepsRepositoryTopologySeparate(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "LIMIT $provisioned_platform_limit") {
			t.Fatalf("provisioned platform query is unbounded: %s", cypher)
		}
		if got, want := IntVal(params, "provisioned_platform_limit"), contextStoryItemLimit+1; got != want {
			t.Fatalf("provisioned_platform_limit = %d, want %d", got, want)
		}
		return []map[string]any{{
			"platform_source_id": "repository:infra", "platform_source_name": "infra",
			"platform_dependency_target_id": "repository:orders",
			"platform_id":                   "platform:eks:prod", "platform_name": "prod", "platform_kind": "kubernetes",
			"dependency_edge": map[string]any{"source_fact_id": "fact-dependency"},
			"platform_edge":   map[string]any{"source_fact_id": "fact-platform"},
		}}, nil
	}}
	handler := &EntityHandler{Neo4j: reader}

	result, err := handler.fetchProvisionedPlatformResult(t.Context(), "repository:orders")
	if err != nil {
		t.Fatalf("fetchProvisionedPlatformResult() error = %v", err)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got, want := StringVal(result.rows[0], "topology_basis"), "provisioning_fallback"; got != want {
		t.Fatalf("topology_basis = %q, want %q", got, want)
	}
	edges := mapSliceValue(result.rows[0], "topology_edges")
	assertExactTopologyEdge(t, edges, "PROVISIONS_DEPENDENCY_FOR", "repository:infra", "repository:orders", "fact-dependency")
	assertExactTopologyEdge(t, edges, "PROVISIONS_PLATFORM", "repository:infra", "platform:eks:prod", "fact-platform")
}

func TestFetchProvisionedPlatformsOrdersSamePlatformByRepositoryEndpoints(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{
			{
				"platform_source_id": "repository:z-infra", "platform_dependency_target_id": "repository:orders",
				"platform_id": "platform:eks:prod", "platform_name": "prod",
			},
			{
				"platform_source_id": "repository:a-infra", "platform_dependency_target_id": "repository:orders",
				"platform_id": "platform:eks:prod", "platform_name": "prod",
			},
		}, nil
	}}

	result, err := (&EntityHandler{Neo4j: reader}).fetchProvisionedPlatformResult(t.Context(), "repository:orders")
	if err != nil {
		t.Fatalf("fetchProvisionedPlatformResult() error = %v", err)
	}
	if got, want := len(result.rows), 2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	for index, want := range []string{"repository:a-infra", "repository:z-infra"} {
		edges := mapSliceValue(result.rows[index], "topology_edges")
		if got := StringVal(edges[0], "source_id"); got != want {
			t.Fatalf("rows[%d] source_id = %q, want %q", index, got, want)
		}
	}
}

func TestBuildDeploymentTraceResponseDoesNotCopyProvisioningUnderInstances(t *testing.T) {
	t.Parallel()

	got := buildDeploymentTraceResponse("orders", map[string]any{
		"id": "workload:orders", "name": "orders",
		"instances": []map[string]any{{"instance_id": "instance:orders:prod", "platforms": []map[string]any{}}},
		"provisioned_platforms": []map[string]any{{
			"platform_id": "platform:eks:prod",
			"topology_edges": []map[string]any{{
				"relationship_type": "PROVISIONS_PLATFORM", "source_id": "repository:infra", "target_id": "platform:eks:prod",
			}},
		}},
	})
	instances := mapSliceValue(got, "instances")
	if gotPlatforms := mapSliceValue(instances[0], "platforms"); len(gotPlatforms) != 0 {
		t.Fatalf("instances[0].platforms = %#v, want direct RUNS_ON only", gotPlatforms)
	}
	if gotProvisioned := mapSliceValue(got, "provisioned_platforms"); len(gotProvisioned) != 1 {
		t.Fatalf("provisioned_platforms = %#v, want separate collection", gotProvisioned)
	}
}
