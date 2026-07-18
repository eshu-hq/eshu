// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

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
			"coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason": {{
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
			"coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason": {{
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
