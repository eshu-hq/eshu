package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveEntityAcceptsRepositorySelectorAlias(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{{
					"id":         "entity-1",
					"labels":     []any{"Function"},
					"name":       "handler",
					"file_path":  "src/app.go",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "go",
					"start_line": int64(10),
					"end_line":   int64(20),
				}}, nil
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo-1",
				Name:      "payments",
				LocalPath: "/repos/payments",
				RepoSlug:  "acme/payments",
			}},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"handler","type":"function","repo_id":"acme/payments"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
