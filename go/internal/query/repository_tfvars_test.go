package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryContextSurfacesTerraformVariableFileRelationship(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-live",
					"name":             "live-config",
					"path":             "/repos/live-config",
					"local_path":       "/repos/live-config",
					"remote_url":       "https://github.com/acme/live-config",
					"repo_slug":        "acme/live-config",
					"has_remote":       true,
					"file_count":       int64(6),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(1),
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN type(rel) AS type": {
					{
						"type":          "PROVISIONS_DEPENDENCY_FOR",
						"target_name":   "payments-service",
						"target_id":     "repo-payments",
						"evidence_type": "terraform",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-live/context", nil)
	req.SetPathValue("repo_id", "repo-live")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	relationships, ok := response["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", response["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1", len(relationships))
	}

	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationships[0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "PROVISIONS_DEPENDENCY_FOR"; got != want {
		t.Fatalf("relationships[0].type = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "payments-service"; got != want {
		t.Fatalf("relationships[0].target_name = %#v, want %#v", got, want)
	}
	if got, want := relationship["evidence_type"], "terraform"; got != want {
		t.Fatalf("relationships[0].evidence_type = %#v, want %#v", got, want)
	}
}
