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

func TestFetchDeploymentSourcesReturnsExactRelationshipEndpoints(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {{
				"instance_id": "instance:runtime-deploy:prod",
				"repo_id":     "repo-runtime-deploy",
				"repo_name":   "runtime-deploy",
				"confidence":  0.97,
				"reason":      "canonical_instance_deployment_source",
			}},
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {{
				"repo_id":    "repo-legacy-deploy",
				"repo_name":  "legacy-deploy",
				"confidence": 0.62,
				"reason":     "repository_deploys_from",
			}},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSourcesFromGraph() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSourcesFromGraph()) = %d, want 2", len(got))
	}
	assertDeploymentSourceEndpoints(
		t,
		got[0],
		"DEPLOYMENT_SOURCE",
		"instance:runtime-deploy:prod",
		"repo-runtime-deploy",
	)
	assertDeploymentSourceEndpoints(
		t,
		got[1],
		"DEPLOYS_FROM",
		"repo-legacy-deploy",
		"repository:r_service_edge_api",
	)
}

func TestFetchDeploymentSourcesRejectsNonFiniteConfidence(t *testing.T) {
	t.Parallel()

	_, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {{
				"instance_id": "instance:runtime-deploy:prod",
				"repo_id":     "repo-runtime-deploy",
				"confidence":  math.Inf(1),
			}},
		},
	}, "workload:service-edge-api", "")
	if err == nil || !strings.Contains(err.Error(), "deployment source confidence") {
		t.Fatalf("fetchDeploymentSourcesFromGraph() error = %v, want non-finite deployment-source confidence error", err)
	}
}

func TestFetchDeploymentSourcesPreservesRelationshipFamiliesForSameRepository(t *testing.T) {
	t.Parallel()

	got, err := fetchDeploymentSourcesFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {{
				"instance_id": "instance:runtime-deploy:prod",
				"repo_id":     "repo-runtime-deploy",
				"repo_name":   "runtime-deploy",
				"confidence":  0.97,
				"reason":      "canonical_instance_deployment_source",
			}},
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {{
				"repo_id":    "repo-runtime-deploy",
				"repo_name":  "runtime-deploy",
				"confidence": 0.62,
				"reason":     "repository_deploys_from",
			}},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSourcesFromGraph() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSourcesFromGraph()) = %d, want 2 relationship families", len(got))
	}
	if got[0]["relationship_type"] != "DEPLOYMENT_SOURCE" || got[1]["relationship_type"] != "DEPLOYS_FROM" {
		t.Fatalf("relationship families = %#v, want DEPLOYMENT_SOURCE and DEPLOYS_FROM", got)
	}
}

func TestFetchDeploymentSourcesRejectsRelationshipsWithoutCanonicalEndpoints(t *testing.T) {
	t.Parallel()

	result, err := fetchDeploymentSourceResultFromGraph(t.Context(), fakeRepoGraphReader{
		runByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {{
				"repo_id":   "repository:deploy",
				"repo_name": "deploy",
			}},
		},
	}, "workload:orders", "")
	if err != nil {
		t.Fatalf("fetchDeploymentSourceResultFromGraph() error = %v", err)
	}
	if len(result.rows) != 0 {
		t.Fatalf("deployment source rows = %#v, want malformed endpoint row rejected", result.rows)
	}
	if got := IntVal(result.limits, "canonical_observed_count"); got != 0 {
		t.Fatalf("canonical_observed_count = %d, want malformed row excluded", got)
	}
}

func TestFetchDeploymentSourcesOrdersCanonicalEndpointTiesDeterministically(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "DEPLOYMENT_SOURCE") &&
			!strings.Contains(cypher, "ORDER BY repo_name, instance_id, repo_id") {
			t.Fatalf("canonical deployment-source query lacks endpoint tie-breakers:\n%s", cypher)
		}
		if strings.Contains(cypher, "targetRepo:Repository") && !strings.Contains(cypher, "EvidenceArtifact") &&
			!strings.Contains(cypher, "ORDER BY repo_name, repo_id") {
			t.Fatalf("repository deployment-source query lacks identity tie-breakers:\n%s", cypher)
		}
		return []map[string]any{}, nil
	}}
	if _, err := fetchDeploymentSourcesFromGraph(
		t.Context(),
		reader,
		"workload:service-edge-api",
		"repository:r_service_edge_api",
	); err != nil {
		t.Fatalf("fetchDeploymentSourcesFromGraph() error = %v, want nil", err)
	}
}

func TestFetchDeploymentSourcesScopesEveryRepositoryEndpointBeforeLimit(t *testing.T) {
	t.Parallel()

	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repository:allowed"},
	})
	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		limitIndex := strings.Index(cypher, "LIMIT $source_limit")
		if limitIndex < 0 {
			t.Fatalf("deployment-source query is missing its bound:\n%s", cypher)
		}
		for _, want := range []string{"repo.id IN $allowed_repository_ids", "repo.id IN $allowed_scope_ids"} {
			if predicateIndex := strings.Index(cypher, want); predicateIndex < 0 || predicateIndex > limitIndex {
				t.Fatalf("deployment-source query does not scope source repository before LIMIT with %q:\n%s", want, cypher)
			}
		}
		if strings.Contains(cypher, "targetRepo:Repository") {
			for _, want := range []string{"targetRepo.id IN $allowed_repository_ids", "targetRepo.id IN $allowed_scope_ids"} {
				if predicateIndex := strings.Index(cypher, want); predicateIndex < 0 || predicateIndex > limitIndex {
					t.Fatalf("repository deployment-source query does not scope target before LIMIT with %q:\n%s", want, cypher)
				}
			}
		}
		if got := params["allowed_repository_ids"]; fmt.Sprint(got) != "[repository:allowed]" {
			t.Fatalf("allowed_repository_ids = %#v, want the caller's repository grant", got)
		}
		if got := params["allowed_scope_ids"]; fmt.Sprint(got) != "[]" {
			t.Fatalf("allowed_scope_ids = %#v, want an explicit empty scope grant", got)
		}

		// An out-of-grant source exists in the graph. A correctly scoped query
		// cannot return it, while the pre-fix query would expose it here.
		return nil, nil
	}}

	result, err := fetchDeploymentSourceResultFromGraph(
		ctx,
		reader,
		"workload:orders",
		"repository:allowed",
	)
	if err != nil {
		t.Fatalf("fetchDeploymentSourceResultFromGraph() error = %v", err)
	}
	if len(result.rows) != 0 {
		t.Fatalf("deployment source rows = %#v, want no cross-grant repositories", result.rows)
	}
}

func TestFetchDeploymentSourcesBoundsGraphExpansionAndReportsCoverage(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "LIMIT $source_limit") {
			t.Fatalf("deployment-source query is unbounded:\n%s", cypher)
		}
		if got, want := IntVal(params, "source_limit"), contextStoryItemLimit+1; got != want {
			t.Fatalf("source_limit = %d, want sentinel limit %d", got, want)
		}
		if !strings.Contains(cypher, "DEPLOYMENT_SOURCE") {
			return nil, nil
		}
		rows := make([]map[string]any, 0, contextStoryItemLimit+1)
		for index := range contextStoryItemLimit + 1 {
			rows = append(rows, map[string]any{
				"instance_id": fmt.Sprintf("instance:%03d", index),
				"repo_id":     fmt.Sprintf("repository:%03d", index),
				"repo_name":   fmt.Sprintf("deployment-%03d", index),
			})
		}
		return rows, nil
	}}

	result, err := fetchDeploymentSourceResultFromGraph(
		t.Context(),
		reader,
		"workload:service-edge-api",
		"repository:r_service_edge_api",
	)
	if err != nil {
		t.Fatalf("fetchDeploymentSourceResultFromGraph() error = %v, want nil", err)
	}
	if got, want := len(result.rows), contextStoryItemLimit; got != want {
		t.Fatalf("deployment source rows = %d, want capped %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") {
		t.Fatalf("deployment source limits = %#v, want truncated", result.limits)
	}
	if got, want := IntVal(result.limits, "returned_count"), contextStoryItemLimit; got != want {
		t.Fatalf("returned_count = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "observed_count"), contextStoryItemLimit+1; got != want {
		t.Fatalf("observed_count = %d, want sentinel lower bound %d", got, want)
	}
	if !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("deployment source limits = %#v, want lower-bound disclosure", result.limits)
	}
}

func TestFetchDeploymentSourcesDeduplicatesEndpointsBeforeSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "DEPLOYMENT_SOURCE") {
			if !strings.Contains(cypher, "WITH i.id as instance_id, repo.id as repo_id") {
				t.Fatalf("canonical query does not group unique endpoints before LIMIT: %s", cypher)
			}
			return []map[string]any{{"instance_id": "instance:prod", "repo_id": "repository:deploy", "repo_name": "deploy"}}, nil
		}
		if strings.Contains(cypher, "EvidenceArtifact") {
			return nil, nil
		}
		if !strings.Contains(cypher, "WITH repo.id as repo_id") {
			t.Fatalf("repository query does not group unique endpoints before LIMIT: %s", cypher)
		}
		return nil, nil
	}}

	result, err := fetchDeploymentSourceResultFromGraph(t.Context(), reader, "workload:orders", "repository:orders")
	if err != nil {
		t.Fatalf("fetchDeploymentSourceResultFromGraph() error = %v", err)
	}
	if BoolVal(result.limits, "truncated") {
		t.Fatalf("limits = %#v, want one unique endpoint complete", result.limits)
	}
}

func assertDeploymentSourceEndpoints(
	t *testing.T,
	row map[string]any,
	relationshipType string,
	sourceID string,
	targetID string,
) {
	t.Helper()
	if row["relationship_type"] != relationshipType || row["source_id"] != sourceID || row["target_id"] != targetID {
		t.Fatalf(
			"deployment source endpoints = %#v, want %s %s -> %s",
			row,
			relationshipType,
			sourceID,
			targetID,
		)
	}
}
