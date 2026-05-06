package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleLanguageQuery_TypeScriptFunctionUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["language"], "typescript"; got != want {
					t.Fatalf("params[language] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.decorators as decorators",
					"e.type_parameters as type_parameters",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}
				return []map[string]any{
					{
						"entity_id":       "graph-ts-function-1",
						"name":            "identity",
						"labels":          []any{"Function"},
						"file_path":       "src/decorators.ts",
						"repo_id":         "repo-1",
						"repo_name":       "repo-1",
						"language":        "typescript",
						"start_line":      int64(4),
						"end_line":        int64(8),
						"decorators":      []any{"@sealed"},
						"type_parameters": []any{"T"},
					},
				}, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		strings.NewReader(`{"language":"typescript","entity_type":"function","query":"identity","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed TypeScript function", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function identity uses decorators @sealed and declares type parameters T."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "generic_declaration"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := typescriptSemantics["decorators"], []any{"@sealed"}; !equalAnySlice(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := typescriptSemantics["type_parameters"], []any{"T"}; !equalAnySlice(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
}

func equalAnySlice(value any, want []any) bool {
	got, ok := value.([]any)
	if !ok {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
