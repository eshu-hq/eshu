package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestResolveEntityRanksCanonicalServiceEntitiesAheadOfAnonymousDirectories(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (e) WHERE e.name = $name"):
					if got, want := params["name"], "service-edge-api"; got != want {
						t.Fatalf("params[name] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"id":     "",
							"labels": []any{"Directory"},
							"name":   "service-edge-api",
						},
						{
							"id":         "content-entity:e_chart",
							"labels":     []any{"HelmChart"},
							"name":       "service-edge-api",
							"file_path":  "charts/service-edge-api/Chart.yaml",
							"repo_id":    "repository:r_helm",
							"repo_name":  "deployment-helm",
							"language":   "yaml",
							"start_line": int64(1),
							"end_line":   int64(8),
						},
						{
							"id":        "repository:r_service_edge_api",
							"labels":    []any{"Repository"},
							"name":      "service-edge-api",
							"repo_id":   "",
							"repo_name": "",
						},
						{
							"id":     "workload:service-edge-api",
							"labels": []any{"Workload"},
							"name":   "service-edge-api",
						},
						{
							"id":     "workload-instance:service-edge-api:modern",
							"labels": []any{"WorkloadInstance"},
							"name":   "service-edge-api",
						},
						{
							"id":     "",
							"labels": []any{"Directory"},
							"name":   "service-edge-api",
						},
					}, nil
				case strings.Contains(cypher, "OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(direct:Workload)"):
					return []map[string]any{
						{
							"entity_id": "workload:service-edge-api",
							"repo_id":   "repository:r_service_edge_api",
							"repo_name": "service-edge-api",
						},
						{
							"entity_id": "workload-instance:service-edge-api:modern",
							"repo_id":   "repository:r_service_edge_api",
							"repo_name": "service-edge-api",
						},
					}, nil
				default:
					t.Fatalf("unexpected cypher = %q", cypher)
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"service-edge-api"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	entities, ok := resp["entities"].([]any)
	if !ok {
		t.Fatalf("entities type = %T, want []any", resp["entities"])
	}
	if got, want := len(entities), 4; got != want {
		t.Fatalf("len(entities) = %d, want %d", got, want)
	}

	first, _ := entities[0].(map[string]any)
	second, _ := entities[1].(map[string]any)
	third, _ := entities[2].(map[string]any)
	if got, want := first["id"], "repository:r_service_edge_api"; got != want {
		t.Fatalf("entities[0].id = %#v, want %#v", got, want)
	}
	if got, want := second["id"], "workload:service-edge-api"; got != want {
		t.Fatalf("entities[1].id = %#v, want %#v", got, want)
	}
	if got, want := third["id"], "workload-instance:service-edge-api:modern"; got != want {
		t.Fatalf("entities[2].id = %#v, want %#v", got, want)
	}

	for _, raw := range entities {
		entity, _ := raw.(map[string]any)
		if gotID, gotRepoID, gotFile := entity["id"], entity["repo_id"], entity["file_path"]; gotID == "" && gotRepoID == "" && gotFile == "" {
			t.Fatalf("anonymous entity leaked into response: %#v", entity)
		}
	}
}

func TestResolveEntityBackfillsRepoIdentityForCanonicalMatches(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (e) WHERE e.name = $name"):
					if got, want := params["name"], "service-edge-api"; got != want {
						t.Fatalf("params[name] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"id":        "repository:r_service_edge_api",
							"labels":    []any{"Repository"},
							"name":      "service-edge-api",
							"repo_id":   "",
							"repo_name": "",
						},
						{
							"id":        "workload:service-edge-api",
							"labels":    []any{"Workload"},
							"name":      "service-edge-api",
							"repo_id":   "",
							"repo_name": "",
						},
						{
							"id":        "workload-instance:service-edge-api:modern",
							"labels":    []any{"WorkloadInstance"},
							"name":      "service-edge-api",
							"repo_id":   "",
							"repo_name": "",
						},
					}, nil
				case strings.Contains(cypher, "OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(direct:Workload)"):
					if got, want := params["entity_ids"], []string{
						"workload-instance:service-edge-api:modern",
						"workload:service-edge-api",
					}; !reflect.DeepEqual(got, want) {
						t.Fatalf("params[entity_ids] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"entity_id": "workload:service-edge-api",
							"repo_id":   "repository:r_service_edge_api",
							"repo_name": "service-edge-api",
						},
						{
							"entity_id": "workload-instance:service-edge-api:modern",
							"repo_id":   "repository:r_service_edge_api",
							"repo_name": "service-edge-api",
						},
					}, nil
				default:
					t.Fatalf("unexpected cypher = %q", cypher)
					return nil, nil
				}
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"service-edge-api"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	entities, ok := resp["entities"].([]any)
	if !ok {
		t.Fatalf("entities type = %T, want []any", resp["entities"])
	}
	if got, want := len(entities), 3; got != want {
		t.Fatalf("len(entities) = %d, want %d", got, want)
	}

	for _, raw := range entities {
		entity, _ := raw.(map[string]any)
		if got, want := entity["repo_name"], "service-edge-api"; got != want {
			t.Fatalf("entity.repo_name = %#v, want %#v for %#v", got, want, entity)
		}
	}

	first, _ := entities[0].(map[string]any)
	if got, want := first["repo_id"], "repository:r_service_edge_api"; got != want {
		t.Fatalf("entities[0].repo_id = %#v, want %#v", got, want)
	}
	if got, want := first["repo_name"], "service-edge-api"; got != want {
		t.Fatalf("entities[0].repo_name = %#v, want %#v", got, want)
	}
}

func TestResolveEntityReplacesGraphProjectionPlaceholdersWithContentRepoIdentity(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (e) WHERE e.name = $name") {
					t.Fatalf("cypher = %q, want entity resolve lookup", cypher)
				}
				if got, want := params["name"], "handleRelationships"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"id":         "content-entity:e_handle",
						"labels":     []any{"Function"},
						"name":       "handleRelationships",
						"file_path":  "go/internal/query/code_relationships.go",
						"repo_id":    "r.id",
						"repo_name":  "r.name",
						"language":   "go",
						"start_line": int64(22),
						"end_line":   int64(168),
					},
				}, nil
			},
		},
		Content: resolvingEntityContentStore{
			entitiesByID: map[string]EntityContent{
				"content-entity:e_handle": {
					EntityID:     "content-entity:e_handle",
					RepoID:       "repository:r_eshu",
					RelativePath: "go/internal/query/code_relationships.go",
					EntityType:   "Function",
					EntityName:   "handleRelationships",
					StartLine:    22,
					EndLine:      168,
					Language:     "go",
				},
			},
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:r_eshu", Name: "eshu"},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"handleRelationships"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one entity", resp["entities"])
	}
	entity, _ := entities[0].(map[string]any)
	if got, want := entity["repo_id"], "repository:r_eshu"; got != want {
		t.Fatalf("entity.repo_id = %#v, want %#v", got, want)
	}
	if got, want := entity["repo_name"], "eshu"; got != want {
		t.Fatalf("entity.repo_name = %#v, want %#v", got, want)
	}
}

type resolvingEntityContentStore struct {
	fakePortContentStore
	entitiesByID map[string]EntityContent
	repositories []RepositoryCatalogEntry
}

func (s resolvingEntityContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	entity, ok := s.entitiesByID[entityID]
	if !ok {
		return nil, nil
	}
	return &entity, nil
}

func (s resolvingEntityContentStore) ListRepositories(context.Context) ([]RepositoryCatalogEntry, error) {
	return append([]RepositoryCatalogEntry(nil), s.repositories...), nil
}
