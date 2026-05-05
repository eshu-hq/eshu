package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRepositoryContextSurfacesDockerComposeDependsOnRelationship(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-deploy", "docker-compose.yaml", "abc123", "",
					"hash-docker-compose", int64(24), "yaml", "docker_compose",
				},
			},
		},
	})

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"INSTANCE_OF": {
					"id":               "repo-deploy",
					"name":             "compose-deploy",
					"path":             "/repos/compose-deploy",
					"local_path":       "/repos/compose-deploy",
					"remote_url":       "https://github.com/acme/compose-deploy",
					"repo_slug":        "acme/compose-deploy",
					"has_remote":       true,
					"file_count":       int64(1),
					"workload_count":   int64(0),
					"platform_count":   int64(0),
					"dependency_count": int64(1),
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN type(rel) AS type": {
					{
						"type":          "DEPENDS_ON",
						"target_name":   "checkout-service",
						"target_id":     "repo-checkout",
						"evidence_type": "docker_compose",
					},
				},
			},
		},
		Content: NewContentReader(db),
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-deploy/context", nil)
	req.SetPathValue("repo_id", "repo-deploy")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("relationships type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1", len(relationships))
	}
	rel, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationships[0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := rel["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationships[0].type = %#v, want %#v", got, want)
	}
	if got, want := rel["target_name"], "checkout-service"; got != want {
		t.Fatalf("relationships[0].target_name = %#v, want %#v", got, want)
	}
	if got, want := rel["evidence_type"], "docker_compose"; got != want {
		t.Fatalf("relationships[0].evidence_type = %#v, want %#v", got, want)
	}
}
