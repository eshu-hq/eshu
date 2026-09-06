// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestFetchDeploymentSourcesFallsBackToRepositoryDeployEdgesWhenNoCanonicalSourcesExist(t *testing.T) {
	t.Parallel()

	got, err := FetchDeploymentSourcesFromGraph(t.Context(), querytestutil.FakeRepoGraphReader{
		RunByMatch: map[string][]map[string]any{
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
				{
					"repo_id":    "repo-helm",
					"repo_name":  "deployment-helm",
					"confidence": 0.93,
					"reason":     "helm_values_reference",
				},
				{
					"repo_id":    "repo-kustomize",
					"repo_name":  "deployment-kustomize",
					"confidence": 0.91,
					"reason":     "kustomize_resource_reference",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2", len(got))
	}
	if got[0]["repo_name"] != "deployment-helm" {
		t.Fatalf("fetchDeploymentSources()[0].repo_name = %#v, want %#v", got[0]["repo_name"], "deployment-helm")
	}
	if got[0]["reason"] != "helm_values_reference" {
		t.Fatalf("fetchDeploymentSources()[0].reason = %#v, want %#v", got[0]["reason"], "helm_values_reference")
	}
	if got[1]["repo_name"] != "deployment-kustomize" {
		t.Fatalf("fetchDeploymentSources()[1].repo_name = %#v, want %#v", got[1]["repo_name"], "deployment-kustomize")
	}
	if got[1]["reason"] != "kustomize_resource_reference" {
		t.Fatalf("fetchDeploymentSources()[1].reason = %#v, want %#v", got[1]["reason"], "kustomize_resource_reference")
	}
}

func TestFetchDeploymentSourcesMergesCanonicalAndRepositorySources(t *testing.T) {
	t.Parallel()

	got, err := FetchDeploymentSourcesFromGraph(t.Context(), querytestutil.FakeRepoGraphReader{
		RunByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {
				{
					"instance_id": "instance:runtime-deploy:prod",
					"repo_id":     "repo-runtime-deploy",
					"repo_name":   "runtime-deploy",
					"confidence":  0.97,
					"reason":      "canonical_instance_deployment_source",
				},
			},
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
				{
					"repo_id":    "repo-legacy-deploy",
					"repo_name":  "legacy-deploy",
					"confidence": 0.62,
					"reason":     "repository_deploys_from",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2", len(got))
	}
	if got[0]["repo_name"] != "runtime-deploy" {
		t.Fatalf("fetchDeploymentSources()[0].repo_name = %#v, want %#v", got[0]["repo_name"], "runtime-deploy")
	}
	if got[1]["repo_name"] != "legacy-deploy" {
		t.Fatalf("fetchDeploymentSources()[1].repo_name = %#v, want %#v", got[1]["repo_name"], "legacy-deploy")
	}
}

func TestFetchDeploymentSourcesPreservesCanonicalAndRepositoryRelationshipOverlap(t *testing.T) {
	t.Parallel()

	got, err := FetchDeploymentSourcesFromGraph(t.Context(), querytestutil.FakeRepoGraphReader{
		RunByMatch: map[string][]map[string]any{
			"MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)": {
				{
					"instance_id": "instance:runtime-deploy:prod",
					"repo_id":     "repo-runtime-deploy",
					"repo_name":   "runtime-deploy",
					"confidence":  0.97,
					"reason":      "canonical_instance_deployment_source",
				},
			},
			"min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason": {
				{
					"repo_id":    "repo-runtime-deploy",
					"repo_name":  "runtime-deploy",
					"confidence": 0.62,
					"reason":     "repository_deploys_from",
				},
			},
		},
	}, "workload:service-edge-api", "repository:r_service_edge_api")
	if err != nil {
		t.Fatalf("fetchDeploymentSources() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchDeploymentSources()) = %d, want 2 exact relationship families", len(got))
	}
	if got[0]["reason"] != "canonical_instance_deployment_source" {
		t.Fatalf("fetchDeploymentSources()[0].reason = %#v, want %#v", got[0]["reason"], "canonical_instance_deployment_source")
	}
}

func TestFetchDeploymentSourceResultReportsFirstHopSaturationWhenTargetExpansionReturnsFewerRows(t *testing.T) {
	t.Parallel()

	expansionCalled := false
	result, err := FetchDeploymentSourceResultFromGraph(t.Context(), querytestutil.FakeRepoGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "DEPLOYMENT_SOURCE"):
			return nil, nil
		case strings.Contains(cypher, "[rel:DEPLOYS_FROM]"):
			return []map[string]any{{"repo_id": "repo-deploy", "repo_name": "deploy", "confidence": 0.99}}, nil
		case strings.Contains(cypher, "artifact.id AS artifact_id"):
			// The first hop reached the 51-row sentinel, but only 50 artifacts
			// expanded to the requested target. Saturation must not disappear.
			rows := make([]map[string]any, querycontract.ContextStoryItemLimit+1)
			for i := range rows {
				rows[i] = map[string]any{
					"source_id": "repo-deploy", "artifact_id": fmt.Sprintf("artifact-%02d", i),
					"flux_git_repository_namespace": "flux-system",
					"flux_git_repository_name":      fmt.Sprintf("source-%02d", i),
				}
			}
			return rows, nil
		case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"):
			expansionCalled = true
			return make([]map[string]any, querycontract.ContextStoryItemLimit), nil
		default:
			return nil, nil
		}
	}}, "workload-app", "repo-app")
	if err != nil {
		t.Fatal(err)
	}
	if expansionCalled {
		t.Fatal("target expansion ran after first-hop saturation; sentinel artifact could enter attribution")
	}
	if !querycontract.BoolVal(result.limits, "flux_target_binding_observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want first-hop saturation lower bound", result.limits)
	}
	if len(result.rows) != 1 || !querycontract.BoolVal(result.rows[0], "flux_target_bindings_saturated") {
		t.Fatalf("rows = %#v, want saturated source with no partial attribution", result.rows)
	}
}
