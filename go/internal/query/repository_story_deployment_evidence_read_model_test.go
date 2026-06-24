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

func TestGetRepositoryStoryUsesReadModelDeploymentEvidence(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": {
					"id":         "repo-service",
					"name":       "checkout-service",
					"path":       "/repos/checkout-service",
					"local_path": "/repos/checkout-service",
					"has_remote": false,
				},
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "EvidenceArtifact") {
					t.Fatalf("cypher = %q, want repository story to use deployment evidence read model before graph fallback", cypher)
				}
				return nil, nil
			},
		},
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available: true,
				FileCount: 4,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 2},
					{Language: "yaml", FileCount: 2},
				},
			},
			summary: repositoryReadModelSummary{
				Available:     true,
				WorkloadNames: []string{"checkout-service"},
			},
			deploymentEvidence: repositoryDeploymentEvidenceReadModel{
				Available: true,
				Rows: []map[string]any{
					{
						"direction":             "incoming",
						"artifact_id":           "evidence-artifact:terraform:1",
						"name":                  "environments/prod/ecs.tf",
						"domain":                "deployment",
						"path":                  "environments/prod/ecs.tf",
						"evidence_kind":         "TERRAFORM_ECS_SERVICE",
						"artifact_family":       "terraform",
						"extractor":             "terraform-runtime-service-module",
						"relationship_type":     "PROVISIONS_DEPENDENCY_FOR",
						"resolved_id":           "resolved-runtime",
						"generation_id":         "gen-runtime",
						"confidence":            0.96,
						"environment":           "prod",
						"runtime_platform_kind": "ecs",
						"source_repo_id":        "repo-platform",
						"source_repo_name":      "runtime-platform",
						"target_repo_id":        "repo-service",
						"target_repo_name":      "checkout-service",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-service/story", nil)
	req.SetPathValue("repo_id", "repo-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if containsStringAny(resp["limitations"].([]any), "deployment_surface_unknown") {
		t.Fatalf("limitations = %#v, must not claim deployment surface unknown when read-model deployment evidence exists", resp["limitations"])
	}
	deploymentOverview := resp["deployment_overview"].(map[string]any)
	if got, want := deploymentOverview["deployment_evidence_artifact_count"], float64(1); got != want {
		t.Fatalf("deployment_overview.deployment_evidence_artifact_count = %#v, want %#v", got, want)
	}
	if !containsStringAny(deploymentOverview["deployment_tool_families"].([]any), "terraform") {
		t.Fatalf("deployment_overview.deployment_tool_families = %#v, want terraform", deploymentOverview["deployment_tool_families"])
	}
	if len(deploymentOverview["delivery_paths"].([]any)) == 0 {
		t.Fatalf("deployment_overview.delivery_paths = %#v, want read-model deployment evidence path", deploymentOverview["delivery_paths"])
	}
}

func TestBuildRepositoryStoryResponseSummarizesRepositoryOnlyDeploymentEvidence(t *testing.T) {
	t.Parallel()

	got := buildRepositoryStoryResponse(
		RepoRef{ID: "repo-deploy-only", Name: "deployment-only"},
		3,
		[]string{"yaml"},
		nil,
		nil,
		0,
		map[string]any{
			"deployment_evidence": map[string]any{
				"artifact_count":    1,
				"artifact_families": []string{"helm"},
				"artifacts": []map[string]any{
					{
						"id":                "evidence-artifact:helm:1",
						"artifact_family":   "helm",
						"evidence_kind":     "HELM_VALUES_REFERENCE",
						"path":              "environments/prod/values.yaml",
						"relationship_type": "DEPLOYS_FROM",
						"source_repo_id":    "repo-deploy-only",
						"target_repo_id":    "repo-service",
					},
				},
			},
		},
		nil,
	)

	limitations := got["limitations"].([]string)
	if containsString(limitations, "deployment_surface_unknown") {
		t.Fatalf("limitations = %#v, must not claim deployment unknown when repository-only deployment evidence exists", limitations)
	}
	if !containsString(limitations, "workload_surface_unknown") {
		t.Fatalf("limitations = %#v, want workload surface still marked unknown", limitations)
	}
	deploymentOverview := got["deployment_overview"].(map[string]any)
	if got, want := deploymentOverview["deployment_evidence_artifact_count"], 1; got != want {
		t.Fatalf("deployment_evidence_artifact_count = %#v, want %#v", got, want)
	}
	if len(mapSliceValue(deploymentOverview, "delivery_paths")) == 0 {
		t.Fatalf("delivery_paths = %#v, want repository-only deployment evidence path", deploymentOverview["delivery_paths"])
	}
}
