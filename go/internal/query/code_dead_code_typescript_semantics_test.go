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

func TestHandleDeadCodeReturnsGraphBackedTypeScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "e.decorators as decorators") {
					t.Fatalf("cypher = %q, want graph semantic projection", cypher)
				}
				return []map[string]any{
					{
						"entity_id":               "class-ts-1",
						"name":                    "Service",
						"labels":                  []any{"Class"},
						"file_path":               "src/service.ts",
						"repo_id":                 "repo-1",
						"repo_name":               "repo-1",
						"language":                "typescript",
						"start_line":              int64(1),
						"end_line":                int64(12),
						"decorators":              []any{"@sealed"},
						"type_parameters":         []any{"T"},
						"declaration_merge_group": "Service",
						"declaration_merge_count": int64(2),
						"declaration_merge_kinds": []any{"class", "namespace"},
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
		t.Fatalf("results = %#v, want one graph-backed TypeScript result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	semantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := semantics["decorators"], []any{"@sealed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := semantics["type_parameters"], []any{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
}
