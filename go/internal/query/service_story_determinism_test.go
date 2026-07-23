// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Issue #5644: repeated authorized service-story calls over unchanged
// retained data returned the same evidence values in different array order.
// The differing paths were confined to runtime instances, attached
// platforms + topology edges, and consumer repositories, plus prose derived
// from those arrays. These tests feed identical evidence in different
// backend row orders -- simulating a backend that does not guarantee stable
// ORDER BY replay across calls -- and assert the built collections and the
// full service-story payload are byte-identical regardless of row order.

func TestFetchWorkloadRuntimeTopologySortsInstancesByEnvironmentAndIdentity(t *testing.T) {
	t.Parallel()

	rowDevA := workloadRuntimeTopologyTestRow("dev", "workload-instance:orders:dev-a")
	rowProdA := workloadRuntimeTopologyTestRow("prod", "workload-instance:orders:prod-a")
	rowProdB := workloadRuntimeTopologyTestRow("prod", "workload-instance:orders:prod-b")

	ascending := []map[string]any{rowDevA, rowProdA, rowProdB}
	shuffled := []map[string]any{rowProdB, rowDevA, rowProdA}

	wantOrder := []string{
		"workload-instance:orders:dev-a",
		"workload-instance:orders:prod-a",
		"workload-instance:orders:prod-b",
	}

	for name, rows := range map[string][]map[string]any{"ascending": ascending, "shuffled": shuffled} {
		rows := rows
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			}}
			result, err := fetchWorkloadRuntimeTopology(
				t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders-api"},
				"repository:orders",
			)
			if err != nil {
				t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
			}
			got := instanceIDs(result.instances)
			if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
				t.Fatalf("instance order (%s) = %v, want %v", name, got, wantOrder)
			}
		})
	}
}

// TestFetchWorkloadRuntimeTopologyTruncationSurvivorSetIsOrderIndependentAboveLimit
// proves #5644's fix: when a workload has more distinct instances than
// contextStoryItemLimit, the fixed-size survivor set (and the edges built
// from it) must not depend on the order the backend happens to return rows
// in. Before the fix, fetchWorkloadRuntimeTopology capped `instances` to the
// limit INSIDE the row-walking loop and only sorted afterward, so the 50
// survivors were whichever 50 distinct instance_ids were encountered first
// in backend row order -- not the lexicographically-first 50.
func TestFetchWorkloadRuntimeTopologyTruncationSurvivorSetIsOrderIndependentAboveLimit(t *testing.T) {
	t.Parallel()

	const instanceCount = contextStoryItemLimit + 1
	ascending := make([]map[string]any, 0, instanceCount)
	for i := 1; i <= instanceCount; i++ {
		ascending = append(ascending, workloadRuntimeTopologyTestRow("prod", fmt.Sprintf("workload-instance:orders:inst-%02d", i)))
	}
	reversed := make([]map[string]any, len(ascending))
	for i, row := range ascending {
		reversed[len(ascending)-1-i] = row
	}

	ascendingResult := buildDeterminismRuntimeTopology(t, ascending)
	reversedResult := buildDeterminismRuntimeTopology(t, reversed)

	if len(ascendingResult.instances) != contextStoryItemLimit {
		t.Fatalf("ascending survivor count = %d, want %d", len(ascendingResult.instances), contextStoryItemLimit)
	}
	if len(reversedResult.instances) != contextStoryItemLimit {
		t.Fatalf("reversed survivor count = %d, want %d", len(reversedResult.instances), contextStoryItemLimit)
	}

	ascendingIDs := instanceIDs(ascendingResult.instances)
	reversedIDs := instanceIDs(reversedResult.instances)
	if strings.Join(ascendingIDs, ",") != strings.Join(reversedIDs, ",") {
		t.Fatalf("survivor set depends on backend row order:\nascending = %v\nreversed  = %v", ascendingIDs, reversedIDs)
	}

	// Every test row shares one workload ("workload:orders-api") and one repo
	// ("repository:orders"), so the full distinct edge set is exactly one
	// DEFINES edge (repo -> workload, shared by all instances) plus one
	// INSTANCE_OF edge per distinct instance (51 before truncation). After
	// truncation to contextStoryItemLimit (50) survivors, the DEFINES edge is
	// still retained (it is backed by every surviving instance), but the
	// dropped 51st instance's own INSTANCE_OF edge must not survive. If
	// retainTopologyEdgesForInstances were removed, the untruncated 52-edge
	// set (1 DEFINES + 51 INSTANCE_OF) would ship instead, so this count
	// assertion fails without that filtering step.
	wantTopologyEdgeCount := 1 + contextStoryItemLimit
	if len(ascendingResult.topologyEdges) != wantTopologyEdgeCount {
		t.Fatalf("ascending topology edge count = %d, want %d", len(ascendingResult.topologyEdges), wantTopologyEdgeCount)
	}
	if len(reversedResult.topologyEdges) != wantTopologyEdgeCount {
		t.Fatalf("reversed topology edge count = %d, want %d", len(reversedResult.topologyEdges), wantTopologyEdgeCount)
	}
	assertRuntimeTopologyEdgeShape(t, "ascending", ascendingResult.topologyEdges, instanceCount)
	assertRuntimeTopologyEdgeShape(t, "reversed", reversedResult.topologyEdges, instanceCount)

	ascendingHash := hashWorkloadRuntimeTopologyResult(t, ascendingResult)
	reversedHash := hashWorkloadRuntimeTopologyResult(t, reversedResult)
	if ascendingHash != reversedHash {
		t.Fatalf("payload hash for reversed backend row order = %s, want %s (same as ascending order)", reversedHash, ascendingHash)
	}
}

// assertRuntimeTopologyEdgeShape asserts the truncated topology-edge set
// contains exactly one DEFINES edge (the workload shared by every fixture
// row) and exactly contextStoryItemLimit INSTANCE_OF edges (one per retained
// instance), and that the dropped instanceCount'th instance's own
// INSTANCE_OF edge is absent -- i.e. no dangling edge for a truncated
// instance leaked into the response.
func assertRuntimeTopologyEdgeShape(t *testing.T, name string, edges []map[string]any, instanceCount int) {
	t.Helper()

	definesCount, instanceOfCount := 0, 0
	droppedInstanceID := fmt.Sprintf("workload-instance:orders:inst-%02d", instanceCount)
	for _, edge := range edges {
		switch StringVal(edge, "relationship_type") {
		case "DEFINES":
			definesCount++
		case "INSTANCE_OF":
			instanceOfCount++
			if StringVal(edge, "source_id") == droppedInstanceID {
				t.Fatalf("%s topology edges retained a dangling INSTANCE_OF edge for dropped instance %s", name, droppedInstanceID)
			}
		}
	}
	if definesCount != 1 {
		t.Fatalf("%s DEFINES edge count = %d, want 1 (one shared workload)", name, definesCount)
	}
	if instanceOfCount != contextStoryItemLimit {
		t.Fatalf("%s INSTANCE_OF edge count = %d, want %d (one per retained instance)", name, instanceOfCount, contextStoryItemLimit)
	}
}

func buildDeterminismRuntimeTopology(t *testing.T, rows []map[string]any) workloadRuntimeTopologyResult {
	t.Helper()
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return rows, nil
	}}
	result, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": "workload:orders-api"},
		"repository:orders",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	return result
}

func hashWorkloadRuntimeTopologyResult(t *testing.T, result workloadRuntimeTopologyResult) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"instances":      result.instances,
		"topology_edges": result.topologyEdges,
		"limits":         result.limits,
	})
	if err != nil {
		t.Fatalf("json.Marshal(topology result) error = %v", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func TestAttachDirectPlatformsOrdersPlatformsByStableIdentity(t *testing.T) {
	t.Parallel()

	instanceProdA := "workload-instance:orders:prod-a"
	instanceProdB := "workload-instance:orders:prod-b"

	// newRows returns fresh row maps per subtest. fetchWorkloadPlatformResult
	// mutates each row (it sets row["platform_edge"]), so sharing map objects
	// across the two t.Parallel() subtests would be a concurrent map
	// read/write. Each subtest must own its own maps.
	newRows := func(order string) []map[string]any {
		rowEKS := map[string]any{"instance_id": instanceProdA, "platform_id": "platform:eks-prod", "platform_name": "eks-prod", "platform_kind": "argocd_applicationset"}
		rowECS := map[string]any{"instance_id": instanceProdA, "platform_id": "platform:ecs-prod", "platform_name": "ecs-prod", "platform_kind": "ecs_service"}
		rowOther := map[string]any{"instance_id": instanceProdB, "platform_id": "platform:eks-prod-2", "platform_name": "eks-prod-2", "platform_kind": "argocd_applicationset"}
		if order == "shuffled" {
			return []map[string]any{rowOther, rowEKS, rowECS}
		}
		return []map[string]any{rowECS, rowEKS, rowOther}
	}

	wantProdAPlatforms := []string{"platform:ecs-prod", "platform:eks-prod"}

	for _, name := range []string{"ascending", "shuffled"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := newRows(name)
			reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return rows, nil
			}}
			handler := &EntityHandler{Neo4j: reader}
			instances := []map[string]any{
				{"instance_id": instanceProdA},
				{"instance_id": instanceProdB},
			}
			platformResult, err := handler.fetchWorkloadPlatformResult(t.Context(), "repository:orders", "workload:orders-api", instances)
			if err != nil {
				t.Fatalf("fetchWorkloadPlatformResult() error = %v", err)
			}
			attachDirectPlatforms(instances, platformResult.rows)

			got := platformIDs(platformTargets(instances[0]))
			if strings.Join(got, ",") != strings.Join(wantProdAPlatforms, ",") {
				t.Fatalf("platform order (%s) = %v, want %v", name, got, wantProdAPlatforms)
			}
		})
	}
}

// TestSortWorkloadPlatformRowsBreaksTiesByPlatformKindWhenPlatformIDEmpty
// covers a residual tie sortWorkloadPlatformRows left unbroken: the
// production Cypher aggregates by (instance_id, platform_id, platform_name,
// platform_kind), so two rows can share instance_id, platform_name, and an
// empty platform_id while differing only by platform_kind. Without
// platform_kind as a final tiebreaker, those two rows compare equal and
// sort.Slice's order for them is unspecified.
func TestSortWorkloadPlatformRowsBreaksTiesByPlatformKindWhenPlatformIDEmpty(t *testing.T) {
	t.Parallel()

	ecsRow := map[string]any{"instance_id": "workload-instance:orders:prod-a", "platform_id": "", "platform_name": "", "platform_kind": "ecs_service"}
	argoRow := map[string]any{"instance_id": "workload-instance:orders:prod-a", "platform_id": "", "platform_name": "", "platform_kind": "argocd_applicationset"}

	ascending := []map[string]any{argoRow, ecsRow}
	shuffled := []map[string]any{ecsRow, argoRow}

	wantOrder := []string{"argocd_applicationset", "ecs_service"}

	for name, rows := range map[string][]map[string]any{"ascending": ascending, "shuffled": shuffled} {
		rows := append([]map[string]any(nil), rows...)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sortWorkloadPlatformRows(rows)
			got := make([]string, 0, len(rows))
			for _, row := range rows {
				got = append(got, StringVal(row, "platform_kind"))
			}
			if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
				t.Fatalf("platform_kind order (%s) = %v, want %v", name, got, wantOrder)
			}
		})
	}
}

func TestLoadConsumerRepositoryEnrichmentFromCandidatesBreaksTiesByRepoID(t *testing.T) {
	t.Parallel()

	// Both candidates share the same relationship types (so the same
	// consumer_kinds and sort score) and the same repository display name,
	// leaving repo_id as the only remaining stable tiebreaker.
	candidateA := provisioningRepositoryCandidate{RepoID: "repository:consumer-a", RepoName: "orders-consumer", RelationshipTypes: []string{"DEPLOYS_FROM"}}
	candidateB := provisioningRepositoryCandidate{RepoID: "repository:consumer-b", RepoName: "orders-consumer", RelationshipTypes: []string{"DEPLOYS_FROM"}}

	ascending := []provisioningRepositoryCandidate{candidateA, candidateB}
	shuffled := []provisioningRepositoryCandidate{candidateB, candidateA}

	wantOrder := []string{"repository:consumer-a", "repository:consumer-b"}

	for name, candidates := range map[string][]provisioningRepositoryCandidate{"ascending": ascending, "shuffled": shuffled} {
		candidates := candidates
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			consumers, err := loadConsumerRepositoryEnrichmentFromCandidates(
				t.Context(), nil, nil, "repository:orders", "orders-api", nil, 0, candidates,
			)
			if err != nil {
				t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v", err)
			}
			got := make([]string, 0, len(consumers))
			for _, consumer := range consumers {
				got = append(got, StringVal(consumer, "repo_id"))
			}
			if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
				t.Fatalf("consumer order (%s) = %v, want %v", name, got, wantOrder)
			}
		})
	}
}

// TestServiceStoryPayloadHashIsDeterministicAcrossBackendRowOrder proves the
// full get_service_story payload for a representative rich service hashes
// identically whether the backend returns runtime-instance, platform, and
// consumer-repository rows in ascending or shuffled order, and that hashing
// the same build twice reproduces the same value.
func TestServiceStoryPayloadHashIsDeterministicAcrossBackendRowOrder(t *testing.T) {
	t.Parallel()

	ascendingHashFirst := buildDeterminismServiceStoryPayloadHash(t, false)
	ascendingHashSecond := buildDeterminismServiceStoryPayloadHash(t, false)
	shuffledHash := buildDeterminismServiceStoryPayloadHash(t, true)

	if ascendingHashFirst != ascendingHashSecond {
		t.Fatalf("repeated ascending-order build hash = %s, want %s (idempotent rebuild)", ascendingHashSecond, ascendingHashFirst)
	}
	if ascendingHashFirst != shuffledHash {
		t.Fatalf("payload hash for shuffled backend row order = %s, want %s (same as ascending order)", shuffledHash, ascendingHashFirst)
	}
}

func buildDeterminismServiceStoryPayloadHash(t *testing.T, shuffle bool) string {
	t.Helper()
	repoID := "repository:orders"
	workloadID := "workload:orders-api"

	runtimeRows := []map[string]any{
		workloadRuntimeTopologyTestRow("dev", "workload-instance:orders:dev-a"),
		workloadRuntimeTopologyTestRow("prod", "workload-instance:orders:prod-a"),
		workloadRuntimeTopologyTestRow("prod", "workload-instance:orders:prod-b"),
	}
	platformRows := []map[string]any{
		{"instance_id": "workload-instance:orders:prod-a", "platform_id": "platform:ecs-prod", "platform_name": "ecs-prod", "platform_kind": "ecs_service"},
		{"instance_id": "workload-instance:orders:prod-a", "platform_id": "platform:eks-prod", "platform_name": "eks-prod", "platform_kind": "argocd_applicationset"},
		{"instance_id": "workload-instance:orders:prod-b", "platform_id": "platform:eks-prod-2", "platform_name": "eks-prod-2", "platform_kind": "argocd_applicationset"},
	}
	candidates := []provisioningRepositoryCandidate{
		{RepoID: "repository:consumer-a", RepoName: "orders-consumer", RelationshipTypes: []string{"DEPLOYS_FROM"}},
		{RepoID: "repository:consumer-b", RepoName: "orders-consumer", RelationshipTypes: []string{"DEPLOYS_FROM"}},
	}

	if shuffle {
		runtimeRows = []map[string]any{runtimeRows[2], runtimeRows[0], runtimeRows[1]}
		platformRows = []map[string]any{platformRows[2], platformRows[1], platformRows[0]}
		candidates = []provisioningRepositoryCandidate{candidates[1], candidates[0]}
	}

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "RUNS_ON") {
			return platformRows, nil
		}
		return runtimeRows, nil
	}}

	topology, err := fetchWorkloadRuntimeTopology(
		t.Context(), reader, "w.id = $workload_id", map[string]any{"workload_id": workloadID}, repoID,
	)
	if err != nil {
		t.Fatalf("fetchWorkloadRuntimeTopology() error = %v", err)
	}
	handler := &EntityHandler{Neo4j: reader}
	platformResult, err := handler.fetchWorkloadPlatformResult(t.Context(), repoID, workloadID, topology.instances)
	if err != nil {
		t.Fatalf("fetchWorkloadPlatformResult() error = %v", err)
	}
	attachDirectPlatforms(topology.instances, platformResult.rows)

	consumers, err := loadConsumerRepositoryEnrichmentFromCandidates(
		t.Context(), nil, nil, repoID, "orders-api", nil, 0, candidates,
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v", err)
	}

	workloadContext := map[string]any{
		"id":                    "workload:orders-api",
		"name":                  "orders-api",
		"kind":                  "service",
		"repo_id":               repoID,
		"repo_name":             "orders",
		"instances":             topology.instances,
		"topology_edges":        topology.topologyEdges,
		"consumer_repositories": consumers,
	}

	response := buildServiceStoryResponse("orders-api", workloadContext)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(response) error = %v", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func workloadRuntimeTopologyTestRow(environment, instanceID string) map[string]any {
	return map[string]any{
		"repo_id": "repository:orders", "repo_name": "orders",
		"workload_id": "workload:orders-api", "workload_name": "orders-api",
		"instance_id": instanceID, "environment": environment,
		"defines_edge":  map[string]any{"confidence": 0.9, "source_fact_id": "fact-defines:" + instanceID},
		"instance_edge": map[string]any{"confidence": 0.9, "source_fact_id": "fact-instance:" + instanceID},
	}
}

func instanceIDs(instances []map[string]any) []string {
	ids := make([]string, 0, len(instances))
	for _, instance := range instances {
		ids = append(ids, StringVal(instance, "instance_id"))
	}
	return ids
}

func platformIDs(platforms []map[string]any) []string {
	ids := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		ids = append(ids, StringVal(platform, "platform_id"))
	}
	return ids
}
