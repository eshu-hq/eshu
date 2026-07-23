// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
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

func TestFetchProvisionedPlatformsRejectsNonJSONRelationshipProperties(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"platform_source_id": "repository:infra", "platform_dependency_target_id": "repository:orders",
			"platform_id": "platform:eks:prod", "platform_name": "prod",
			"dependency_edges": []map[string]any{{"confidence": math.Inf(1)}},
			"platform_edge":    map[string]any{"source_fact_id": "fact-platform"},
		}}, nil
	}}

	_, err := (&EntityHandler{Neo4j: reader}).fetchProvisionedPlatformResult(t.Context(), "repository:orders")
	if err == nil || !strings.Contains(err.Error(), "marshal graph relationship properties") {
		t.Fatalf("fetchProvisionedPlatformResult() error = %v, want non-JSON relationship-property error", err)
	}
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

// TestFetchProvisionedPlatformsTruncationSurvivorSetIsOrderIndependentAboveLimit
// proves #5644's fix applies to provisioned platforms too: when raw rows
// (bounded by LIMIT $provisioned_platform_limit = contextStoryItemLimit+1)
// contain more distinct (platform_id, source_id, target_id) tuples than
// contextStoryItemLimit, the retained survivor set must not depend on the
// order the backend happens to return rows in. Before the fix,
// fetchProvisionedPlatformResult capped `ordered` to the limit INSIDE the
// row-walking loop and only sorted the already-truncated subset afterward,
// so the 50 survivors were whichever 50 distinct tuples were encountered
// first in backend row order -- not the lexicographically-first 50 by
// provisionedPlatformOrderKey.
func TestFetchProvisionedPlatformsTruncationSurvivorSetIsOrderIndependentAboveLimit(t *testing.T) {
	t.Parallel()

	const platformCount = contextStoryItemLimit + 1
	ascending := make([]map[string]any, 0, platformCount)
	for index := 1; index <= platformCount; index++ {
		id := fmt.Sprintf("platform:%03d", index)
		ascending = append(ascending, map[string]any{
			"platform_source_id": "repository:infra", "platform_dependency_target_id": "repository:orders",
			"platform_id": id, "platform_name": fmt.Sprintf("platform-%03d", index),
		})
	}
	reversed := make([]map[string]any, len(ascending))
	for i, row := range ascending {
		reversed[len(ascending)-1-i] = row
	}

	ascendingResult := buildDeterminismProvisionedPlatforms(t, ascending)
	reversedResult := buildDeterminismProvisionedPlatforms(t, reversed)

	if len(ascendingResult.rows) != contextStoryItemLimit {
		t.Fatalf("ascending survivor count = %d, want %d", len(ascendingResult.rows), contextStoryItemLimit)
	}
	if len(reversedResult.rows) != contextStoryItemLimit {
		t.Fatalf("reversed survivor count = %d, want %d", len(reversedResult.rows), contextStoryItemLimit)
	}

	ascendingIDs := provisionedPlatformIDs(ascendingResult.rows)
	reversedIDs := provisionedPlatformIDs(reversedResult.rows)
	if strings.Join(ascendingIDs, ",") != strings.Join(reversedIDs, ",") {
		t.Fatalf("survivor set depends on backend row order:\nascending = %v\nreversed  = %v", ascendingIDs, reversedIDs)
	}

	ascendingHash := hashProvisionedPlatformResult(t, ascendingResult)
	reversedHash := hashProvisionedPlatformResult(t, reversedResult)
	if ascendingHash != reversedHash {
		t.Fatalf("payload hash for reversed backend row order = %s, want %s (same as ascending order)", reversedHash, ascendingHash)
	}
}

func buildDeterminismProvisionedPlatforms(t *testing.T, rows []map[string]any) provisionedPlatformResult {
	t.Helper()
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return rows, nil
	}}
	result, err := (&EntityHandler{Neo4j: reader}).fetchProvisionedPlatformResult(t.Context(), "repository:orders")
	if err != nil {
		t.Fatalf("fetchProvisionedPlatformResult() error = %v", err)
	}
	return result
}

func hashProvisionedPlatformResult(t *testing.T, result provisionedPlatformResult) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"rows":   result.rows,
		"limits": result.limits,
	})
	if err != nil {
		t.Fatalf("json.Marshal(provisioned platform result) error = %v", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func provisionedPlatformIDs(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, StringVal(row, "platform_id"))
	}
	return ids
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
