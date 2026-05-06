package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLanguageQuery_TSXComponentUsesGraphFirstPath(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{
			rows: []map[string]any{
				{
					"entity_id":  "component-1",
					"name":       "Button",
					"labels":     []any{"Component"},
					"file_path":  "src/Button.tsx",
					"repo_id":    "repo-1",
					"repo_name":  "repo-1",
					"language":   "tsx",
					"start_line": int64(1),
					"end_line":   int64(12),
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"component","query":"Button","repo_id":"repo-1"}`),
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
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed component", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "component-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["name"], "Button"; got != want {
		t.Fatalf("result[name] = %#v, want %#v", got, want)
	}
}
