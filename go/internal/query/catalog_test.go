package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListCatalogReturnsRepositoriesWorkloadsAndServices(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("limit param = %#v, want %#v", got, want)
				}
				switch {
				case strings.Contains(cypher, "MATCH (r:Repository)"):
					return []map[string]any{
						{
							"id":         "repository:r_api",
							"name":       "api-node-boats",
							"local_path": "/repos/api-node-boats",
						},
					}, nil
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return []map[string]any{
						{
							"id":             "workload:api-node-boats",
							"name":           "api-node-boats",
							"kind":           "service",
							"repo_id":        "repository:r_api",
							"repo_name":      "api-node-boats",
							"instance_count": int64(2),
							"environments":   []any{"prod", "qa"},
						},
						{
							"id":             "workload:nightly-sync",
							"name":           "nightly-sync",
							"kind":           "cronjob",
							"repo_id":        "repository:r_api",
							"repo_name":      "api-node-boats",
							"instance_count": int64(1),
							"environments":   []any{"prod"},
						},
					}, nil
				default:
					return nil, nil
				}
			},
		},
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=2", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := body["count"], float64(3); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := body["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}

	assertCatalogCollectionLength(t, body, "repositories", 1)
	assertCatalogCollectionLength(t, body, "workloads", 2)
	assertCatalogCollectionLength(t, body, "services", 1)
}

func TestListCatalogTruncatesEachCollectionByLimit(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (r:Repository)"):
					return []map[string]any{
						{"id": "repository:r_1", "name": "one"},
						{"id": "repository:r_2", "name": "two"},
					}, nil
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return []map[string]any{
						{"id": "workload:w_1", "name": "one", "kind": "service"},
						{"id": "workload:w_2", "name": "two", "kind": "service"},
					}, nil
				default:
					return nil, nil
				}
			},
		},
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=1", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := body["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	assertCatalogCollectionLength(t, body, "repositories", 1)
	assertCatalogCollectionLength(t, body, "workloads", 1)
	assertCatalogCollectionLength(t, body, "services", 1)
}

func TestListCatalogIncludesIdentityOnlyServicesFromReadModel(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			workloadIdentities: []CatalogWorkloadIdentityEntry{
				{
					Name:     "api-node-boats",
					RepoID:   "repository:r_api",
					RepoName: "api-node-boats",
				},
			},
		},
		Neo4j: fakeRepoGraphReader{
			runByMatch: map[string][]map[string]any{
				"MATCH (r:Repository)": {},
				"MATCH (w:Workload)":   {},
			},
		},
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	assertCatalogCollectionLength(t, body, "services", 1)
	services := body["services"].([]any)
	service := services[0].(map[string]any)
	if got, want := service["name"], "api-node-boats"; got != want {
		t.Fatalf("service name = %#v, want %#v", got, want)
	}
	if got, want := service["materialization_status"], "identity_only"; got != want {
		t.Fatalf("materialization_status = %#v, want %#v", got, want)
	}
}

func assertCatalogCollectionLength(t *testing.T, body map[string]any, key string, want int) {
	t.Helper()
	rows, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, body[key])
	}
	if len(rows) != want {
		t.Fatalf("len(%s) = %d, want %d", key, len(rows), want)
	}
}
