// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestBuildServiceStoryResponseReturnsCompleteDossier(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()

	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	identity := mapValue(got, "service_identity")
	if got, want := StringVal(identity, "service_id"), "workload:sample-service-api"; got != want {
		t.Fatalf("service_identity.service_id = %q, want %q", got, want)
	}
	if got, want := StringVal(identity, "repo_id"), "repo-sample-service-api"; got != want {
		t.Fatalf("service_identity.repo_id = %q, want %q", got, want)
	}

	apiSurface := mapValue(got, "api_surface")
	if got, want := IntVal(apiSurface, "endpoint_count"), 2; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	endpoints := mapSliceValue(apiSurface, "endpoints")
	if len(endpoints) != 2 {
		t.Fatalf("len(api_surface.endpoints) = %d, want 2", len(endpoints))
	}

	lanes := mapSliceValue(got, "deployment_lanes")
	if len(lanes) != 2 {
		t.Fatalf("len(deployment_lanes) = %d, want dual deployment lanes: %#v", len(lanes), lanes)
	}
	gotLaneTypes := []string{StringVal(lanes[0], "lane_type"), StringVal(lanes[1], "lane_type")}
	wantLaneTypes := []string{"ecs_terraform", "k8s_gitops"}
	if !reflect.DeepEqual(gotLaneTypes, wantLaneTypes) {
		t.Fatalf("deployment lane types = %#v, want %#v", gotLaneTypes, wantLaneTypes)
	}

	upstream := mapSliceValue(got, "upstream_dependencies")
	if len(upstream) != 4 {
		t.Fatalf("len(upstream_dependencies) = %d, want 4", len(upstream))
	}
	if got, want := StringVal(upstream[0], "resolved_id"), "resolved-gitops"; got != want {
		t.Fatalf("upstream_dependencies[0].resolved_id = %q, want %q", got, want)
	}

	downstream := mapValue(got, "downstream_consumers")
	if got, want := IntVal(downstream, "graph_dependent_count"), 1; got != want {
		t.Fatalf("downstream_consumers.graph_dependent_count = %d, want %d", got, want)
	}
	if got, want := IntVal(downstream, "content_consumer_count"), 1; got != want {
		t.Fatalf("downstream_consumers.content_consumer_count = %d, want %d", got, want)
	}

	graph := mapValue(got, "evidence_graph")
	edges := mapSliceValue(graph, "edges")
	if len(edges) != 4 {
		t.Fatalf("len(evidence_graph.edges) = %d, want 2 deployment and 2 runtime edges", len(edges))
	}
	deploymentEdges := 0
	runtimeEdges := 0
	for _, edge := range edges {
		if StringVal(edge, "relationship_type") == "RUNS_AS" {
			runtimeEdges++
			continue
		}
		deploymentEdges++
		if StringVal(edge, "resolved_id") == "" {
			t.Fatalf("evidence_graph edge missing resolved_id: %#v", edge)
		}
	}
	if deploymentEdges != 2 || runtimeEdges != 2 {
		t.Fatalf("evidence graph edge roles = deployment:%d runtime:%d, want 2/2", deploymentEdges, runtimeEdges)
	}

	limits := mapValue(got, "result_limits")
	if got, want := limits["truncated"], false; got != want {
		t.Fatalf("result_limits.truncated = %#v, want false", got)
	}
}

func TestBuildServiceStoryResponseKeepsEmptyDossierSections(t *testing.T) {
	t.Parallel()

	got := buildServiceStoryResponse("empty-service", map[string]any{
		"id":        "workload:empty-service",
		"name":      "empty-service",
		"kind":      "service",
		"repo_id":   "repo-empty",
		"repo_name": "empty-service",
		"instances": []map[string]any{},
	})

	for _, key := range []string{
		"service_identity",
		"api_surface",
		"deployment_lanes",
		"upstream_dependencies",
		"downstream_consumers",
		"evidence_graph",
		"result_limits",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("response missing empty dossier key %q: %#v", key, got)
		}
	}
	if got, want := IntVal(mapValue(got, "api_surface"), "endpoint_count"), 0; got != want {
		t.Fatalf("empty api_surface.endpoint_count = %d, want %d", got, want)
	}
	if lanes := mapSliceValue(got, "deployment_lanes"); len(lanes) != 0 {
		t.Fatalf("empty deployment_lanes = %#v, want none", lanes)
	}
}

func TestBuildServiceStoryResponseHandlesSingleDeploymentLane(t *testing.T) {
	t.Parallel()

	got := buildServiceStoryResponse("payments-api", map[string]any{
		"id":        "workload:payments-api",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-payments-api",
		"repo_name": "payments-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:payments-api:prod",
				"platform_name": "payments-prod",
				"platform_kind": "argocd_application",
				"environment":   "production",
			},
		},
	})

	lanes := mapSliceValue(got, "deployment_lanes")
	if len(lanes) != 1 {
		t.Fatalf("len(deployment_lanes) = %d, want 1: %#v", len(lanes), lanes)
	}
	if got, want := StringVal(lanes[0], "lane_type"), "k8s_gitops"; got != want {
		t.Fatalf("deployment_lanes[0].lane_type = %q, want %q", got, want)
	}
}

func TestGetServiceStoryReturnsEnvelopeDataWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":      "workload:service-edge-api",
					"name":    "service-edge-api",
					"kind":    "service",
					"repo_id": "repo-service-edge-api",
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.name = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data == nil {
		t.Fatal("envelope data is nil, want service dossier payload")
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "platform_impact.context_overview" {
		t.Fatalf("truth = %#v, want platform impact context truth", envelope.Truth)
	}
}

func TestGetServiceStoryReturnsEnvelopeErrorWhenServiceMissing(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/missing/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "missing")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data != nil {
		t.Fatalf("envelope data = %#v, want nil", envelope.Data)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("envelope error = %#v, want not_found", envelope.Error)
	}
}

func TestGetServiceStoryReturnsAmbiguousEnvelopeCandidates(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if got, want := params["service_name"], "checkout"; got != want {
					t.Fatalf("params[service_name] = %#v, want %q", got, want)
				}
				return []map[string]any{
					{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api", "environment": "prod"},
					{"id": "workload:checkout-worker", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-worker", "environment": "qa"},
				}, nil
			},
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				t.Fatal("ambiguous service story must not fetch or enrich a random workload")
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeAmbiguous {
		t.Fatalf("envelope error = %#v, want ambiguous", envelope.Error)
	}
	if envelope.Data != nil {
		t.Fatalf("envelope data = %#v, want nil on error", envelope.Data)
	}
	candidates, ok := envelope.Error.Details["candidates"].([]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want 2 candidates", envelope.Error.Details["candidates"])
	}
}

func TestGetServiceStoryRepoSelectorDisambiguatesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{ID: "repo-checkout-api", Name: "checkout-api"}},
		},
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if !strings.Contains(cypher, "w.repo_id = $repo_id") {
					t.Fatalf("service candidate query missing repo filter:\n%s", cypher)
				}
				if got, want := params["repo_id"], "repo-checkout-api"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?repo=checkout-api", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", envelope.Data)
	}
	identity := data["service_identity"].(map[string]any)
	if got, want := identity["repo_id"], "repo-checkout-api"; got != want {
		t.Fatalf("service_identity.repo_id = %#v, want %q", got, want)
	}
}

func TestGetServiceStoryEnvironmentSelectorDisambiguatesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if !strings.Contains(cypher, "WorkloadInstance") || !strings.Contains(cypher, "i.environment = $environment") {
					t.Fatalf("environment selector query missing WorkloadInstance environment filter:\n%s", cypher)
				}
				if got, want := params["environment"], "prod"; got != want {
					t.Fatalf("params[environment] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api", "environment": "prod"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"WorkloadInstance":                    {{"instance_id": "instance:checkout:prod", "environment": "prod"}},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?environment=prod", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetServiceStoryServiceIDSelectorUsesExactWorkload(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.id = $service_id") {
					return nil, nil
				}
				if got, want := params["service_id"], "workload:checkout-api"; got != want {
					t.Fatalf("params[service_id] = %#v, want %q", got, want)
				}
				return []map[string]any{{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"}}, nil
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
			},
			runByMatch: map[string][]map[string]any{
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout/story?service_id=workload%3Acheckout-api", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "checkout")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func sampleServiceDossierContext() map[string]any {
	return map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{"instance_id": "inst-prod", "platform_name": "eks-prod", "platform_kind": "argocd_applicationset", "environment": "production"},
			{"instance_id": "inst-qa", "platform_name": "ecs-qa", "platform_kind": "ecs_service", "environment": "qa"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 2,
			"method_count":   3,
			"spec_count":     1,
			"endpoints": []map[string]any{
				{"path": "/v3/items", "methods": []string{"get"}, "operation_ids": []string{"listItems"}, "spec_path": "specs/index.yaml"},
				{"path": "/v3/items/{id}", "methods": []string{"get", "delete"}, "operation_ids": []string{"getItem", "deleteItem"}, "spec_path": "specs/index.yaml"},
			},
		},
		"documentation_overview": map[string]any{
			"repo_slug":        "example/sample-service-api",
			"docs_route_count": 2,
		},
		"dependencies": []map[string]any{
			{"type": "READS_CONFIG_FROM", "target_name": "config-service", "target_id": "repo-config"},
		},
		"dependents": []map[string]any{
			{"repository": "deployment-helm", "repo_id": "repo-helm", "relationship_types": []string{"DEPLOYS_FROM"}},
		},
		"consumer_repositories": []map[string]any{
			{"repository": "sample-search-api", "repo_id": "repo-search", "evidence_kinds": []string{"hostname_reference"}, "matched_values": []string{"sample-service-api.qa.example.test"}, "sample_paths": []string{"config/qa.json"}},
		},
		"provisioning_source_chains": []map[string]any{
			{"repository": "terraform-runtime", "repo_id": "repo-terraform", "modules": []string{"ecs_service"}},
		},
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{"id": "artifact-gitops", "direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-gitops", "confidence": 0.94, "artifact_family": "argocd", "source_repo_id": "repo-gitops", "source_repo_name": "deployment-charts", "target_repo_id": "repo-sample-service-api", "target_repo_name": "sample-service-api", "path": "argocd/prod/app.yaml"},
				{"id": "artifact-terraform", "direction": "incoming", "relationship_type": "PROVISIONS_DEPENDENCY_FOR", "resolved_id": "resolved-terraform", "confidence": 0.91, "artifact_family": "terraform", "runtime_platform_kind": "ecs_service", "source_repo_id": "repo-terraform", "source_repo_name": "terraform-runtime", "target_repo_id": "repo-sample-service-api", "target_repo_name": "sample-service-api", "path": "env/qa/ecs.tf"},
			},
		},
	}
}
