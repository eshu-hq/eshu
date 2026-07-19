// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetServiceContextOmitsRepoEntryPoints(t *testing.T) {
	t.Parallel()

	entryPointQueried := false
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "service-edge-api"}}, nil
				case strings.Contains(cypher, "fn.name IN"):
					entryPointQueried = true
					return []map[string]any{{"name": "main", "relative_path": "cmd/server/main.go", "language": "go"}}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/context", nil)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, exists := resp["entry_points"]; exists {
		t.Fatalf("entry_points = %#v, want omitted for service context", resp["entry_points"])
	}
	if entryPointQueried {
		t.Fatal("service context queried repo entry points even though enrichment omits them")
	}
}

func TestFetchWorkloadContextAnchorsFollowUpQueriesByResolvedWorkloadID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":   "workload:service-edge-api",
					"name": "service-edge-api",
					"kind": "service",
				},
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "service-edge-api"}}, nil
				}
				if strings.Contains(cypher, "w.name = $service_name OR w.id = $service_name") {
					t.Fatalf("follow-up cypher used broad service lookup after workload id was resolved: %s", cypher)
				}
				if strings.Contains(cypher, "MATCH (w:Workload)") && params["workload_id"] != "workload:service-edge-api" {
					got := params["workload_id"]
					t.Fatalf("params[workload_id] = %#v, want %#v", got, "workload:service-edge-api")
				}
				return nil, nil
			},
		},
	}

	ctx, err := handler.fetchWorkloadContext(
		context.Background(),
		serviceLookupWhereClause,
		map[string]any{"service_name": "service-edge-api"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v, want nil", err)
	}
	if got, want := ctx["repo_id"], "repo-1"; got != want {
		t.Fatalf("ctx[repo_id] = %#v, want %#v", got, want)
	}
}

func TestGetServiceContextIncludesGraphDeploymentEvidenceWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": {
					"id":        "workload:checkout-service",
					"name":      "checkout-service",
					"kind":      "service",
					"repo_id":   "repo-service",
					"repo_name": "checkout-service",
					"instances": []any{},
				},
			},
			runByMatch: map[string][]map[string]any{
				"MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)": {
					{"repo_id": "repo-service", "repo_name": "checkout-service"},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
				"(r:Repository {id: $repo_id})-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->": {
					{
						"direction":         "outgoing",
						"artifact_id":       "evidence-artifact:gha:1",
						"name":              ".github/workflows/deploy.yml",
						"domain":            "deployment",
						"path":              ".github/workflows/deploy.yml",
						"evidence_kind":     "GITHUB_ACTIONS_REUSABLE_WORKFLOW",
						"artifact_family":   "github_actions",
						"extractor":         "github_actions",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-ci",
						"generation_id":     "gen-service",
						"confidence":        0.93,
						"matched_alias":     "shared-workflows",
						"matched_value":     "org/shared-workflows/.github/workflows/deploy.yml@v1",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-service",
						"source_repo_name":  "checkout-service",
						"target_repo_id":    "repo-workflows",
						"target_repo_name":  "shared-workflows",
					},
				},
				"EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository": {
					{
						"direction":         "incoming",
						"artifact_id":       "evidence-artifact:helm:1",
						"name":              "charts/checkout/values-prod.yaml",
						"domain":            "deployment",
						"path":              "charts/checkout/values-prod.yaml",
						"evidence_kind":     "HELM_VALUES_REFERENCE",
						"artifact_family":   "helm",
						"extractor":         "helm",
						"relationship_type": "DEPLOYS_FROM",
						"resolved_id":       "resolved-helm",
						"generation_id":     "gen-deploy",
						"confidence":        0.84,
						"environment":       "prod",
						"matched_alias":     "checkout-service",
						"matched_value":     "registry.example.test/checkout-service",
						"evidence_source":   "resolver/cross-repo",
						"source_repo_id":    "repo-deploy",
						"source_repo_name":  "deployment-configs",
						"target_repo_id":    "repo-service",
						"target_repo_name":  "checkout-service",
					},
				},
				"EXPOSES_ENDPOINT]->(endpoint:Endpoint)": {
					{
						"endpoint_id":     "endpoint:checkout:health",
						"path":            "/health",
						"methods":         []any{"get"},
						"source_kinds":    []any{"framework:express"},
						"source_paths":    []any{"src/server.ts"},
						"evidence_source": "workload_materialization",
						"workload_id":     "workload:checkout-service",
						"workload_name":   "checkout-service",
					},
				},
				"RETURN count(endpoint) AS endpoint_count": {
					{"endpoint_count": 1},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout-service/context", nil)
	req.SetPathValue("service_name", "checkout-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	evidence, ok := resp["deployment_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_evidence type = %T, want map[string]any", resp["deployment_evidence"])
	}
	if got, want := evidence["truth_basis"], "graph"; got != want {
		t.Fatalf("deployment_evidence.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := evidence["artifact_count"], float64(2); got != want {
		t.Fatalf("deployment_evidence.artifact_count = %#v, want %#v", got, want)
	}
	for _, want := range []string{"github_actions", "helm"} {
		if !containsStringAny(evidence["artifact_families"].([]any), want) {
			t.Fatalf("artifact_families missing %q: %#v", want, evidence["artifact_families"])
		}
	}

	apiSurface, ok := resp["api_surface"].(map[string]any)
	if !ok {
		t.Fatalf("api_surface type = %T, want map[string]any", resp["api_surface"])
	}
	if got, want := apiSurface["truth_basis"], "graph"; got != want {
		t.Fatalf("api_surface.truth_basis = %#v, want %#v", got, want)
	}
	if got, want := apiSurface["endpoint_count"], float64(1); got != want {
		t.Fatalf("api_surface.endpoint_count = %#v, want %#v", got, want)
	}
}

func TestGetWorkloadStoryReturnsNotFoundForMissingWorkload(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/unknown/story", nil)
	req.SetPathValue("workload_id", "unknown")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestBuildWorkloadStorySurfacesObservedServiceSignalsWithoutMaterializedInstances(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{},
		"observed_config_environments": []string{
			"production",
			"qa",
		},
		"hostnames": []map[string]any{
			{"hostname": "sample-service-api.qa.example.com"},
			{"hostname": "sample-service-api.production.example.com"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 21,
			"spec_count":     1,
			"docs_routes":    []string{"/_specs"},
		},
	}

	story := buildWorkloadStory(ctx)
	for _, fragment := range []string{
		"No materialized workload instances found.",
		"Observed config environments: production, qa.",
		"Public entrypoints: sample-service-api.production.example.com, sample-service-api.qa.example.com.",
		"API surface exposes 21 endpoint(s) across 1 spec file(s).",
		"Docs routes: /_specs.",
	} {
		if !strings.Contains(story, fragment) {
			t.Fatalf("story = %q, want fragment %q", story, fragment)
		}
	}
}
