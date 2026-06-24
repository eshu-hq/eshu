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

func TestQueryRepoDependenciesReadsDeployableUnitCorrelationEdges(t *testing.T) {
	t.Parallel()

	var observedCypher string
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			observedCypher = cypher
			return []map[string]any{
				{
					"type":              "CORRELATES_DEPLOYABLE_UNIT",
					"target_name":       "deployments",
					"target_id":         "repo-deployments",
					"evidence_type":     "deployable_unit_correlation",
					"resolved_id":       "deployable-unit-correlation:g1:repo-edge-api:edge-api",
					"generation_id":     "g1",
					"confidence":        0.94,
					"evidence_count":    9,
					"evidence_kinds":    []any{"argocd", "deployment_repo", "deployable_unit_key"},
					"resolution_source": "reducer/deployable-unit-correlation",
					"rationale":         "admitted deployable unit correlation",
				},
			}, nil
		},
	}

	got := queryRepoDependencies(context.Background(), reader, map[string]any{"repo_id": "repo-edge-api"})
	if !strings.Contains(observedCypher, "CORRELATES_DEPLOYABLE_UNIT") {
		t.Fatalf("query cypher missing CORRELATES_DEPLOYABLE_UNIT: %s", observedCypher)
	}
	if len(got) != 1 {
		t.Fatalf("relationships = %d, want 1", len(got))
	}
	if gotType := StringVal(got[0], "type"); gotType != "CORRELATES_DEPLOYABLE_UNIT" {
		t.Fatalf("relationship type = %q, want CORRELATES_DEPLOYABLE_UNIT", gotType)
	}
	if gotSource := StringVal(got[0], "resolution_source"); gotSource != "reducer/deployable-unit-correlation" {
		t.Fatalf("resolution_source = %q, want reducer/deployable-unit-correlation", gotSource)
	}
}

func TestGetRepositoryContextMergesDeployableUnitEdgesWithReadModel(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
			}
			return map[string]any{
				"id":         "repo-service",
				"name":       "service",
				"path":       "/repos/service",
				"local_path": "/repos/service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "[rel:CORRELATES_DEPLOYABLE_UNIT]") {
				return nil, nil
			}
			return []map[string]any{
				{
					"direction":         "outgoing",
					"type":              "CORRELATES_DEPLOYABLE_UNIT",
					"source_name":       "service",
					"source_id":         "repo-service",
					"target_name":       "deployments",
					"target_id":         "repo-deployments",
					"evidence_type":     "deployable_unit_correlation",
					"resolved_id":       "deployable-unit-correlation:g1:service:deployments",
					"generation_id":     "g1",
					"confidence":        0.94,
					"evidence_count":    9,
					"evidence_kinds":    []any{"argocd", "deployable_unit_key"},
					"resolution_source": "reducer/deployable-unit-correlation",
				},
			}, nil
		},
	}
	handler := &RepositoryHandler{
		Neo4j: reader,
		Content: fakePortContentStore{
			relationshipReadModel: repositoryRelationshipReadModel{
				Available: true,
				Relationships: []map[string]any{
					{
						"direction":      "outgoing",
						"type":           "DEPLOYS_FROM",
						"source_name":    "service",
						"source_id":      "repo-service",
						"target_name":    "terraform-live",
						"target_id":      "repo-terraform",
						"evidence_type":  "terraform_app_repo",
						"resolved_id":    "resolved-out",
						"generation_id":  "generation-1",
						"confidence":     0.91,
						"evidence_count": 2,
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-service/context", nil)
	req.SetPathValue("repo_id", "repo-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok || len(relationships) != 2 {
		t.Fatalf("relationships = %#v, want read-model and deployable-unit rows", resp["relationships"])
	}
	if got, want := relationships[1].(map[string]any)["type"], "CORRELATES_DEPLOYABLE_UNIT"; got != want {
		t.Fatalf("relationships[1].type = %#v, want %#v", got, want)
	}
}
