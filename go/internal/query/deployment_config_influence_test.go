// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type deploymentConfigInfluenceContentStore struct {
	fakePortContentStore
	gitOpsEntities []EntityContent
	k8sEntities    []EntityContent
}

const (
	deploymentConfigOmittedGitOpsImage        = "registry.example/omitted:latest"
	deploymentConfigBoundedOmittedGitOpsImage = "registry.example/bounded-omitted:latest"
)

func deploymentConfigGitOpsEntities(count int) []EntityContent {
	if count == 0 {
		return nil
	}
	entities := []EntityContent{{
		EntityID:     "argocd:test-service",
		RepoID:       "repo-gitops",
		RelativePath: "apps/test-service/application.yaml",
		EntityType:   "ArgoCDApplication",
		EntityName:   "test-service",
		Metadata:     map[string]any{"source_path": "apps/test-service"},
	}}
	for index := 1; index < count; index++ {
		image := fmt.Sprintf("registry.example/included:%04d", index)
		if index == serviceStoryItemLimit+1 {
			image = deploymentConfigBoundedOmittedGitOpsImage
		}
		if count > repositorySemanticEntityLimit && index == count-1 {
			image = deploymentConfigOmittedGitOpsImage
		}
		entities = append(entities, EntityContent{
			EntityID:     fmt.Sprintf("k8s:%04d", index),
			RepoID:       "repo-gitops",
			RelativePath: fmt.Sprintf("apps/test-service/deploy/%04d.yaml", index),
			EntityType:   "K8sResource",
			EntityName:   "test-service",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{image},
			},
		})
	}
	return entities
}

func (s deploymentConfigInfluenceContentStore) ListRepoEntities(
	_ context.Context,
	_ string,
	limit int,
) ([]EntityContent, error) {
	rows := s.gitOpsEntities
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return append([]EntityContent(nil), rows...), nil
}

func (s deploymentConfigInfluenceContentStore) SearchEntitiesByName(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	rows := s.k8sEntities
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return append([]EntityContent(nil), rows...), nil
}

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
		"deployment_source_limits": deploymentConfigExactDeploymentSourceLimits(),
		"k8s_resource_limits":      deploymentConfigExactK8sResourceLimits(),
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

func TestInvestigateDeploymentConfigInfluenceReturnsConflictForDuplicateWorkloadName(t *testing.T) {
	t.Parallel()

	call := 0
	handler := &ImpactHandler{Neo4j: fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		if strings.Contains(cypher, "w.name = $service_name") {
			call++
			return map[string]any{"id": fmt.Sprintf("workload:orders-%d", call)}, nil
		}
		return nil, nil
	}}}

	w := requestDeploymentConfigInfluence(t, handler, `{"service_name":"orders"}`)

	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestInvestigateDeploymentConfigInfluenceDisclosesSaturatedUpstreamEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		gitOpsCount    int
		wantTruncated  bool
		wantLowerBound bool
	}{
		{name: "saturated_gitops_probe", gitOpsCount: repositorySemanticEntityLimit + 1, wantTruncated: true, wantLowerBound: true},
		{name: "exact_gitops_read", gitOpsCount: 1, wantTruncated: false, wantLowerBound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := makeDeploymentConfigInfluenceHandler()
			handler.Content = deploymentConfigInfluenceContentStore{
				fakePortContentStore: fakePortContentStore{repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "test-service"}}},
				gitOpsEntities:       deploymentConfigGitOpsEntities(tt.gitOpsCount),
				k8sEntities: []EntityContent{{
					EntityID:     "k8s:deployment:test-service",
					RepoID:       "repo-1",
					RelativePath: "deploy/test-service.yaml",
					EntityType:   "K8sResource",
					EntityName:   "test-service",
					Metadata:     map[string]any{"kind": "Deployment", "container_images": []any{"registry.example/direct:latest"}},
				}},
			}
			handler.Neo4j = fakeWorkloadGraphReader{
				runSingleByMatch: map[string]map[string]any{
					"MATCH (w:Workload) WHERE":            {"id": "svc-1", "name": "test-service", "kind": "service", "repo_id": "repo-1"},
					"MATCH (r:Repository {id: $repo_id})": {"repo_name": "test-service"},
				},
				runByMatch: map[string][]map[string]any{
					"INSTANCE_OF":                         {},
					"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
					"K8sResource OR":                      {},
					"fn.name IN":                          {},
					"DEPLOYMENT_SOURCE":                   {{"instance_id": "instance:test-service", "repo_id": "repo-gitops", "repo_name": "gitops"}},
				},
			}

			w := requestDeploymentConfigInfluence(t, handler, `{"service_name":"test-service","limit":100}`)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}

			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			coverage := mapValue(response, "coverage")
			if got := IntVal(coverage, "rendered_target_count"); got >= 100 {
				t.Fatalf("coverage.rendered_target_count = %d, want fewer than requested limit despite upstream saturation", got)
			}
			if got := BoolVal(coverage, "truncated"); got != tt.wantTruncated {
				t.Fatalf("coverage.truncated = %t, want %t; coverage = %#v", got, tt.wantTruncated, coverage)
			}
			if got := BoolVal(coverage, "observed_count_is_lower_bound"); got != tt.wantLowerBound {
				t.Fatalf("coverage.observed_count_is_lower_bound = %t, want %t; coverage = %#v", got, tt.wantLowerBound, coverage)
			}
			limitations := StringSliceVal(response, "limitations")
			if tt.wantTruncated && !containsString(limitations, "k8s_resource_evidence_truncated") {
				t.Fatalf("limitations = %#v, want k8s_resource_evidence_truncated", limitations)
			}
			if !tt.wantTruncated && containsString(limitations, "k8s_resource_evidence_truncated") {
				t.Fatalf("limitations = %#v, want no Kubernetes resource truncation limitation", limitations)
			}
			deploymentSourceLimits := mapValue(response, "deployment_source_limits")
			k8sResourceLimits := mapValue(response, "k8s_resource_limits")
			if got := IntVal(deploymentSourceLimits, "query_sentinel_limit"); got != contextStoryItemLimit+1 {
				t.Fatalf("deployment_source_limits = %#v, want preserved sentinel metadata", deploymentSourceLimits)
			}
			if got := IntVal(k8sResourceLimits, "deployment_source_query_sentinel_limit"); got != repositorySemanticEntityLimit+1 {
				t.Fatalf("k8s_resource_limits = %#v, want preserved deployment-source sentinel metadata", k8sResourceLimits)
			}
			if got := BoolVal(k8sResourceLimits, "truncated"); got != tt.wantTruncated {
				t.Fatalf("k8s_resource_limits.truncated = %t, want %t; limits = %#v", got, tt.wantTruncated, k8sResourceLimits)
			}
			if got := BoolVal(k8sResourceLimits, "deployment_source_observed_count_is_lower_bound"); got != tt.wantLowerBound {
				t.Fatalf("k8s_resource_limits.deployment_source_observed_count_is_lower_bound = %t, want %t; limits = %#v", got, tt.wantLowerBound, k8sResourceLimits)
			}
			if tt.wantTruncated && strings.Contains(w.Body.String(), deploymentConfigOmittedGitOpsImage) {
				t.Fatalf("response includes image from omitted GitOps row: %s", w.Body.String())
			}
			if tt.wantTruncated && strings.Contains(w.Body.String(), deploymentConfigBoundedOmittedGitOpsImage) {
				t.Fatalf("response includes image from GitOps row omitted by the public cap: %s", w.Body.String())
			}
		})
	}
}

func TestInvestigateDeploymentConfigInfluence_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalLightweight}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/deployment-config-influence", strings.NewReader(`{"service_name":"test-service"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

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
