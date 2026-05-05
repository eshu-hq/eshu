package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDeadCodeReturnsGraphBackedJavaScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if want := "e.docstring as docstring"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				return []map[string]any{
					{
						"entity_id":   "js-fn-1",
						"name":        "getTab",
						"labels":      []any{"Function"},
						"file_path":   "src/tab.js",
						"repo_id":     "repo-1",
						"repo_name":   "repo-1",
						"language":    "javascript",
						"start_line":  int64(4),
						"end_line":    int64(8),
						"docstring":   "Returns the active tab.",
						"method_kind": "getter",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed JavaScript result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}

	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "javascript_method"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}

	jsSemantics, ok := result["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[javascript_semantics] type = %T, want map[string]any", result["javascript_semantics"])
	}
	if got, want := jsSemantics["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
	if got, want := jsSemantics["method_kind"], "getter"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
}
