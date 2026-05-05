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

func TestHandleDeadCodeReturnsGraphBackedPythonSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if want := "e.decorators as decorators"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				return []map[string]any{
					{
						"entity_id":             "py-fn-1",
						"name":                  "handler",
						"labels":                []any{"Function"},
						"file_path":             "src/routes.py",
						"repo_id":               "repo-1",
						"repo_name":             "payments",
						"language":              "python",
						"start_line":            int64(10),
						"end_line":              int64(14),
						"decorators":            []any{"@route"},
						"async":                 true,
						"docstring":             "Handles incoming requests.",
						"type_annotation_count": int64(2),
						"type_annotation_kinds": []any{"parameter", "return"},
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
		t.Fatalf("results = %#v, want one graph-backed Python result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function handler is async, uses decorators @route, has parameter and return type annotations, and is documented as \"Handles incoming requests.\"."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}

	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}

	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[python_semantics] type = %T, want map[string]any", result["python_semantics"])
	}
	if got, want := pythonSemantics["docstring"], "Handles incoming requests."; got != want {
		t.Fatalf("python_semantics[docstring] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
}
