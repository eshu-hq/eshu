// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLanguageQuery_TSXFunctionFragmentUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":              "graph-tsx-function-1",
				"name":                   "Screen",
				"labels":                 []string{"Function"},
				"file_path":              "src/Screen.tsx",
				"repo_id":                "repo-1",
				"repo_name":              "repo-1",
				"language":               "tsx",
				"start_line":             int64(7),
				"end_line":               int64(14),
				"jsx_fragment_shorthand": true,
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"function","query":"Screen","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed TSX function", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function Screen uses JSX fragment shorthand."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
}

// TestHandleLanguageQuery_TSXVariableAssertionUsesContentMetadata covers the
// content-backed "variable" path (see language_query_entities.go): plain and
// semantic Variable rows alike are read from content_entities, so a TSX
// component-type-assertion variable's semantic summary must derive from
// EntityContent.Metadata rather than raw graph row columns.
func TestHandleLanguageQuery_TSXVariableAssertionUsesContentMetadata(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Content: &languageQueryContentStore{rows: []EntityContent{
			{
				EntityID:     "content-tsx-variable-1",
				RepoID:       "repo-1",
				RelativePath: "src/Screen.tsx",
				EntityType:   "Variable",
				EntityName:   "Screen",
				StartLine:    3,
				EndLine:      3,
				Language:     "tsx",
				Metadata:     map[string]any{"component_type_assertion": "ComponentType"},
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"variable","query":"Screen","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed TSX variable", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Variable Screen narrows to ComponentType."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_type_assertion"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}
