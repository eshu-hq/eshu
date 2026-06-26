// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildDeploymentConfigInfluenceResponseReturnsPromptReadyFiles(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"id":        "workload:eshu-hqgraph-resolution-engine",
		"name":      "eshu-hqgraph-resolution-engine",
		"kind":      "service",
		"repo_id":   "repo-runtime",
		"repo_name": "eshu",
		"deployment_sources": []map[string]any{{
			"repo_id":   "repo-gitops",
			"repo_name": "platform-gitops",
			"reason":    "helm values reference",
		}},
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "clusters/platform-qa/eshu/values.yaml",
					"artifact_family":  "helm",
					"evidence_kind":    "helm_values_reference",
					"matched_alias":    "image.tag",
					"matched_value":    "ghcr.io/eshu-hq/eshu:1.2.3",
					"environment":      "platform-qa",
					"start_line":       17,
					"end_line":         18,
					"resolved_id":      "rel-image",
				},
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "charts/eshu/templates/deployment.yaml",
					"artifact_family":  "helm",
					"evidence_kind":    "kubernetes_resource_limit",
					"matched_alias":    "resources.limits.cpu",
					"matched_value":    "500m",
					"environment":      "platform-qa",
					"start_line":       44,
					"end_line":         47,
					"resolved_id":      "rel-limit",
				},
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "clusters/platform-qa/eshu/env.yaml",
					"artifact_family":  "argocd",
					"evidence_kind":    "runtime_config_reference",
					"matched_alias":    "env.ESHU_REDUCER_WORKERS",
					"matched_value":    "8",
					"environment":      "platform-qa",
				},
			},
		},
		"k8s_resources": []map[string]any{{
			"id":        "k8s:deployment:resolution-engine",
			"name":      "eshu-hqgraph-resolution-engine",
			"kind":      "Deployment",
			"namespace": "eshu",
		}},
		"image_refs": []string{"ghcr.io/eshu-hq/eshu:1.2.3"},
	}

	resp := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{
		ServiceName: "eshu-hqgraph-resolution-engine",
		Environment: "platform-qa",
		Limit:       10,
	}, workloadContext)

	if got := StringVal(resp, "service_name"); got != "eshu-hqgraph-resolution-engine" {
		t.Fatalf("service_name = %q, want eshu-hqgraph-resolution-engine", got)
	}
	for key := range map[string]struct{}{
		"image_tag_sources":       {},
		"runtime_setting_sources": {},
		"resource_limit_sources":  {},
		"values_layers":           {},
		"rendered_targets":        {},
		"read_first_files":        {},
	} {
		rows := mapSliceValue(resp, key)
		if len(rows) == 0 {
			t.Fatalf("%s is empty, want prompt-ready rows", key)
		}
	}

	readFirst := mapSliceValue(resp, "read_first_files")
	if got := StringVal(readFirst[0], "repo_id"); got == "" {
		t.Fatalf("read_first_files[0].repo_id is empty")
	}
	if got := StringVal(readFirst[0], "relative_path"); got == "" || got[0] == '/' {
		t.Fatalf("read_first_files[0].relative_path = %q, want portable relative path", got)
	}
	if got := StringVal(readFirst[0], "next_call"); got != "get_file_lines" {
		t.Fatalf("read_first_files[0].next_call = %q, want get_file_lines", got)
	}

	coverage := mapValue(resp, "coverage")
	if got := StringVal(coverage, "query_shape"); got != "deployment_config_influence_story" {
		t.Fatalf("coverage.query_shape = %q, want deployment_config_influence_story", got)
	}
	if BoolVal(coverage, "truncated") {
		t.Fatalf("coverage.truncated = true, want false")
	}
	if got := StringSliceVal(resp, "recommended_next_calls"); len(got) == 0 {
		t.Fatalf("recommended_next_calls is empty")
	} else {
		for _, call := range got {
			if strings.Contains(call, "environment context:") {
				t.Fatalf("recommended_next_calls contains non-contract field: %q", call)
			}
		}
	}
}

func TestBuildDeploymentConfigInfluenceResponseUsesServiceStoryDeploymentEvidence(t *testing.T) {
	t.Parallel()

	resp := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{
		ServiceName: "sample-service-api",
		Limit:       10,
	}, sampleServiceDossierContext())

	influencingRepos := mapSliceValue(resp, "influencing_repositories")
	if len(influencingRepos) < 3 {
		t.Fatalf("influencing_repositories = %#v, want service owner plus deployment evidence repos", influencingRepos)
	}
	readFirst := mapSliceValue(resp, "read_first_files")
	if len(readFirst) == 0 {
		t.Fatalf("read_first_files is empty, want deployment evidence file handles")
	}
	valuesLayers := mapSliceValue(resp, "values_layers")
	if len(valuesLayers) == 0 {
		t.Fatalf("values_layers is empty, want service story deployment artifacts as config influence")
	}
	coverage := mapValue(resp, "coverage")
	if got, want := IntVal(coverage, "artifact_candidate_count"), 2; got != want {
		t.Fatalf("coverage.artifact_candidate_count = %d, want %d", got, want)
	}
}

func makeDeploymentConfigInfluenceHandler() *ImpactHandler {
	return &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload) WHERE": {
					"id":      "svc-1",
					"name":    "test-service",
					"kind":    "service",
					"repo_id": "repo-1",
				},
				"MATCH (r:Repository {id: $repo_id})": {
					"repo_name": "test-service",
				},
			},
			runByMatch: map[string][]map[string]any{
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
				"DEPLOYMENT_SOURCE":                   {},
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "test-service"}},
		},
	}
}

func requestDeploymentConfigInfluence(t *testing.T, handler *ImpactHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/deployment-config-influence", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestInvestigateDeploymentConfigInfluenceReturnsEnrichedResponse(t *testing.T) {
	t.Parallel()

	handler := makeDeploymentConfigInfluenceHandler()
	w := requestDeploymentConfigInfluence(t, handler, `{"service_name":"test-service","limit":10}`)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["service_name"], "test-service"; got != want {
		t.Fatalf("service_name = %#v, want %#v", got, want)
	}
	if _, ok := resp["workload_id"]; !ok {
		t.Fatal("response missing workload_id")
	}
	for _, key := range []string{"values_layers", "image_tag_sources", "rendered_targets", "influencing_repositories"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("response missing %s", key)
		}
	}
}

func TestInvestigateDeploymentConfigInfluenceReturns404ForUnknownService(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
	}

	w := requestDeploymentConfigInfluence(t, handler, `{"service_name":"unknown-service","limit":10}`)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestInvestigateDeploymentConfigInfluence_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/deployment-config-influence", strings.NewReader(`{"service_name":"test-service"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.investigateDeploymentConfigInfluence(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"platform_impact.deployment_config_influence"`) {
		t.Fatalf("body = %s, want deployment_config_influence capability", body)
	}
}
