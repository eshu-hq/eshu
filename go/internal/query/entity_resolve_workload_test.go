// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveEntityWorkloadAppliesDefiningRepositoryScopeBeforeLimit(t *testing.T) {
	t.Parallel()
	propertyQuerySeen := false
	definingQuerySeen := false
	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "[:CONTAINS]") {
			t.Fatalf("workload resolver must not use content-entity ownership:\n%s", cypher)
		}
		switch {
		case strings.Contains(cypher, "MATCH (repo:Repository) WHERE repo.id IN $repo_ids"):
			if !strings.Contains(cypher, "repo.id IN $allowed_repository_ids") {
				t.Fatalf("repository hydration query is not scoped:\n%s", cypher)
			}
			return []map[string]any{{"repo_id": "repo-team-a", "repo_name": "payments"}}, nil
		case strings.Contains(cypher, "MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)"):
			definingQuerySeen = true
			if got, want := params["limit"], 6; got != want {
				t.Fatalf("params[limit] = %#v, want %#v", got, want)
			}
			if !strings.Contains(cypher, "repo.id IN $allowed_repository_ids") {
				t.Fatalf("defining repository query is not scoped before LIMIT:\n%s", cypher)
			}
			return []map[string]any{}, nil
		case strings.Contains(cypher, "MATCH (w:Workload)"):
			propertyQuerySeen = true
			if got, want := params["limit"], 6; got != want {
				t.Fatalf("params[limit] = %#v, want %#v", got, want)
			}
			if !strings.Contains(cypher, "w.repo_id IN $allowed_repository_ids") {
				t.Fatalf("workload property query is not scoped before LIMIT:\n%s", cypher)
			}
			return []map[string]any{{
				"id": "workload:payments-api", "labels": []any{"Workload"},
				"name": "Payments API", "repo_id": "repo-team-a",
			}}, nil
		default:
			t.Fatalf("unexpected workload resolver query:\n%s", cypher)
			return nil, nil
		}
	}}
	handler := &EntityHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Payments API","type":"workload","limit":5}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	handler.resolveEntity(rec, req)
	if !propertyQuerySeen || !definingQuerySeen {
		t.Fatalf("query paths seen = property:%t defining:%t, want both", propertyQuerySeen, definingQuerySeen)
	}
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	entities, ok := body["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one workload", body["entities"])
	}
	entity := entities[0].(map[string]any)
	if got, want := entity["repo_id"], "repo-team-a"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := entity["repo_name"], "payments"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
}

func TestResolveEntityWorkloadFallsBackToDefiningRepository(t *testing.T) {
	t.Parallel()
	handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "MATCH (repo:Repository) WHERE repo.id IN $repo_ids"):
			return []map[string]any{{"repo_id": "repo-legacy", "repo_name": "legacy"}}, nil
		case strings.Contains(cypher, "MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)"):
			return []map[string]any{{
				"id": "workload:legacy-api", "labels": []any{"Workload"},
				"name": "Legacy API", "repo_id": "repo-legacy",
			}}, nil
		case strings.Contains(cypher, "MATCH (w:Workload)"):
			return []map[string]any{{
				"id": "workload:legacy-api", "labels": []any{"Workload"},
				"name": "Legacy API", "repo_id": "",
			}}, nil
		default:
			t.Fatalf("unexpected workload resolver query:\n%s", cypher)
			return nil, nil
		}
	}}, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Legacy API","type":"workload","limit":5}`))
	rec := httptest.NewRecorder()
	handler.resolveEntity(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	entities := body["entities"].([]any)
	if got, want := len(entities), 1; got != want {
		t.Fatalf("len(entities) = %d, want %d", got, want)
	}
	entity := entities[0].(map[string]any)
	if got, want := entity["repo_id"], "repo-legacy"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
}

func TestResolveEntityWorkloadPropertyOnlyHydratesRepositoryFromGraph(t *testing.T) {
	t.Parallel()
	handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)"):
			return []map[string]any{}, nil
		case strings.Contains(cypher, "MATCH (w:Workload)"):
			if strings.Contains(cypher, "'' AS repo_name") {
				t.Fatalf("property query must omit the NornicDB placeholder-prone empty projection:\n%s", cypher)
			}
			return []map[string]any{{
				"id": "workload:property-api", "labels": []any{"Workload"},
				"name": "Property API", "repo_id": "repo-property",
			}}, nil
		case strings.Contains(cypher, "MATCH (repo:Repository) WHERE repo.id IN $repo_ids"):
			return []map[string]any{{"repo_id": "repo-property", "repo_name": "property"}}, nil
		default:
			t.Fatalf("unexpected workload resolver query:\n%s", cypher)
			return nil, nil
		}
	}}, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Property API","type":"workload","limit":5}`))
	rec := httptest.NewRecorder()
	handler.resolveEntity(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	entity := body["entities"].([]any)[0].(map[string]any)
	if got, want := entity["repo_name"], "property"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
}

func TestResolveEntityWorkloadDedupesBeforeRepositoryHydration(t *testing.T) {
	t.Parallel()
	handler := &EntityHandler{
		Content: failingListRepositoriesContentStore{},
		Neo4j: fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)"):
				if !strings.Contains(cypher, "min(repo.id)") || !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("DEFINES fallback must limit unique workload identities:\n%s", cypher)
				}
				return []map[string]any{{
					"id": "workload:dual-api", "labels": []any{"Workload"},
					"name": "Dual API", "repo_id": "repo-defines",
				}}, nil
			case strings.Contains(cypher, "MATCH (w:Workload)"):
				return []map[string]any{{
					"id": "workload:dual-api", "labels": []any{"Workload"},
					"name": "Dual API", "repo_id": "repo-dual",
				}}, nil
			case strings.Contains(cypher, "MATCH (repo:Repository) WHERE repo.id IN $repo_ids"):
				return []map[string]any{{"repo_id": "repo-dual", "repo_name": "dual"}}, nil
			default:
				t.Fatalf("unexpected workload resolver query:\n%s", cypher)
				return nil, nil
			}
		}},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Dual API","type":"workload","limit":5}`))
	rec := httptest.NewRecorder()
	handler.resolveEntity(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	entities := body["entities"].([]any)
	if got, want := len(entities), 1; got != want {
		t.Fatalf("len(entities) = %d, want %d", got, want)
	}
	entity := entities[0].(map[string]any)
	if got, want := entity["repo_id"], "repo-dual"; got != want {
		t.Fatalf("repo_id = %#v, want property ownership %#v", got, want)
	}
}

type failingListRepositoriesContentStore struct {
	fakePortContentStore
}

func (failingListRepositoriesContentStore) ListRepositories(context.Context) ([]RepositoryCatalogEntry, error) {
	return nil, errors.New("content repository list unavailable")
}
