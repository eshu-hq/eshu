package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryContextIncludesGraphAPISurface(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-1",
					"name":             "order-service",
					"path":             "/repos/order-service",
					"local_path":       "/repos/order-service",
					"remote_url":       "https://github.com/org/order-service",
					"repo_slug":        "org/order-service",
					"has_remote":       true,
					"file_count":       int64(120),
					"workload_count":   int64(1),
					"platform_count":   int64(1),
					"dependency_count": int64(0),
				},
			},
			runByMatch: map[string][]map[string]any{
				"EXPOSES_ENDPOINT]->(endpoint:Endpoint)": {
					{
						"endpoint_id":     "endpoint:repo-1:catalog",
						"path":            "/api/catalog",
						"methods":         []any{"get", "post"},
						"operation_ids":   []any{"listCatalog"},
						"source_kinds":    []any{"openapi", "framework:nextjs"},
						"source_paths":    []any{"specs/openapi.yaml", "src/app/api/catalog/route.ts"},
						"spec_versions":   []any{"3.1.0"},
						"api_versions":    []any{"v1"},
						"evidence_source": "workload_materialization",
						"workload_id":     "workload:repo-1:order-service",
						"workload_name":   "order-service",
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
	req.SetPathValue("repo_id", "repo-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
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
	if got, want := apiSurface["framework_route_count"], float64(1); got != want {
		t.Fatalf("api_surface.framework_route_count = %#v, want %#v", got, want)
	}
	endpoints, ok := apiSurface["endpoints"].([]any)
	if !ok || len(endpoints) != 1 {
		t.Fatalf("api_surface.endpoints = %#v, want one endpoint", apiSurface["endpoints"])
	}
	endpoint, ok := endpoints[0].(map[string]any)
	if !ok {
		t.Fatalf("api_surface.endpoints[0] type = %T, want map[string]any", endpoints[0])
	}
	if got, want := endpoint["path"], "/api/catalog"; got != want {
		t.Fatalf("endpoint.path = %#v, want %#v", got, want)
	}
	if got, want := endpoint["source"], "graph"; got != want {
		t.Fatalf("endpoint.source = %#v, want %#v", got, want)
	}
	if got, want := endpoint["workload_name"], "order-service"; got != want {
		t.Fatalf("endpoint.workload_name = %#v, want %#v", got, want)
	}
}
