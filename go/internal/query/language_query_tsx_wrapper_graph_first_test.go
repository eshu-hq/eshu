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

// TestHandleLanguageQuery_TSXReactFCWrapperUsesContentBackedPath covers the
// content-backed "variable" path (see language_query_entities.go): a TSX
// React.FC component-type-assertion variable's semantic summary derives from
// EntityContent.Metadata, since "variable" no longer takes the graph-first
// route.
func TestHandleLanguageQuery_TSXReactFCWrapperUsesContentBackedPath(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Content: &languageQueryContentStore{
			rows: []EntityContent{
				{
					EntityID:     "content-variable-1",
					RepoID:       "repo-1",
					RelativePath: "src/Screen.tsx",
					EntityType:   "Variable",
					EntityName:   "Dynamic",
					StartLine:    6,
					EndLine:      6,
					Language:     "tsx",
					Metadata:     map[string]any{"component_type_assertion": "React.FC"},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"variable","query":"Dynamic","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed variable", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Variable Dynamic narrows to React.FC."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_type_assertion"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := typescriptSemantics["component_type_assertion"], "React.FC"; got != want {
		t.Fatalf("typescript_semantics[component_type_assertion] = %#v, want %#v", got, want)
	}
}

// TestHandleLanguageQuery_TSXReactFunctionComponentWrapperUsesContentBackedPath
// covers the content-backed "variable" path (see language_query_entities.go):
// a TSX React.FunctionComponent component-type-assertion variable's semantic
// summary derives from EntityContent.Metadata, since "variable" no longer
// takes the graph-first route.
func TestHandleLanguageQuery_TSXReactFunctionComponentWrapperUsesContentBackedPath(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Content: &languageQueryContentStore{
			rows: []EntityContent{
				{
					EntityID:     "content-variable-2",
					RepoID:       "repo-1",
					RelativePath: "src/Screen.tsx",
					EntityType:   "Variable",
					EntityName:   "Dynamic",
					StartLine:    6,
					EndLine:      6,
					Language:     "tsx",
					Metadata:     map[string]any{"component_type_assertion": "React.FunctionComponent"},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"variable","query":"Dynamic","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed variable", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Variable Dynamic narrows to React.FunctionComponent."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_type_assertion"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := typescriptSemantics["component_type_assertion"], "React.FunctionComponent"; got != want {
		t.Fatalf("typescript_semantics[component_type_assertion] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TSXMemoWrapperUsesGraphFirstPath(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{
			rows: []map[string]any{
				{
					"entity_id":              "component-1",
					"name":                   "MemoButton",
					"labels":                 []any{"Component"},
					"file_path":              "src/Screen.tsx",
					"repo_id":                "repo-1",
					"repo_name":              "repo-1",
					"language":               "tsx",
					"start_line":             int64(3),
					"end_line":               int64(3),
					"framework":              "react",
					"component_wrapper_kind": "memo",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"component","query":"MemoButton","repo_id":"repo-1"}`),
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
	if got, want := result["semantic_summary"], "Component MemoButton is associated with the react framework and is wrapped by memo."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_wrapper"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := typescriptSemantics["component_wrapper_kind"], "memo"; got != want {
		t.Fatalf("typescript_semantics[component_wrapper_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TSXLazyWrapperUsesGraphFirstPath(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{
			rows: []map[string]any{
				{
					"entity_id":              "component-2",
					"name":                   "LazyButton",
					"labels":                 []any{"Component"},
					"file_path":              "src/Screen.tsx",
					"repo_id":                "repo-1",
					"repo_name":              "repo-1",
					"language":               "tsx",
					"start_line":             int64(3),
					"end_line":               int64(3),
					"framework":              "react",
					"component_wrapper_kind": "lazy",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"component","query":"LazyButton","repo_id":"repo-1"}`),
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
	if got, want := result["semantic_summary"], "Component LazyButton is associated with the react framework and is wrapped by lazy."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_wrapper"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	typescriptSemantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := typescriptSemantics["component_wrapper_kind"], "lazy"; got != want {
		t.Fatalf("typescript_semantics[component_wrapper_kind] = %#v, want %#v", got, want)
	}
}
