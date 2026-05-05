package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLanguageQuery_CTypedefPrefersGraphPathAndEnrichesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-typedef-1", "repo-1", "src/types.h", "Typedef", "my_int",
					int64(3), int64(3), "c", "typedef int my_int;", []byte(`{"type":"int"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-typedef-1",
				"name":       "my_int",
				"labels":     []string{"Typedef"},
				"file_path":  "src/types.h",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "c",
				"start_line": int64(3),
				"end_line":   int64(3),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"c","entity_type":"typedef","query":"my_int","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed typedef", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-typedef-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Typedef my_int aliases int."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["type"], "int"; got != want {
		t.Fatalf("metadata[type] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_CTypedefUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-typedef-1",
				"name":       "my_int",
				"labels":     []string{"Typedef"},
				"file_path":  "src/types.h",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "c",
				"start_line": int64(3),
				"end_line":   int64(3),
				"type":       "int",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"c","entity_type":"typedef","query":"my_int","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed typedef", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Typedef my_int aliases int."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["type"], "int"; got != want {
		t.Fatalf("metadata[type] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_SQLTableUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":       "graph-sql-table-1",
				"name":            "invoice",
				"labels":          []string{"SqlTable"},
				"file_path":       "schema/export.sql",
				"repo_id":         "repo-1",
				"repo_name":       "repo-1",
				"language":        "sql",
				"start_line":      int64(64),
				"end_line":        int64(64),
				"qualified_name":  "public.invoice",
				"sql_entity_type": "SqlTable",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"sql","entity_type":"sql_table","query":"invoice","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed SQL table", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-sql-table-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["qualified_name"], "public.invoice"; got != want {
		t.Fatalf("metadata[qualified_name] = %#v, want %#v", got, want)
	}
	if got, want := metadata["sql_entity_type"], "SqlTable"; got != want {
		t.Fatalf("metadata[sql_entity_type] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TypeAliasPrefersGraphPathAndUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":       "graph-typealias-1",
				"name":            "ReadonlyMap",
				"labels":          []any{"TypeAlias"},
				"file_path":       "src/types.ts",
				"repo_id":         "repo-1",
				"repo_name":       "repo-1",
				"language":        "typescript",
				"start_line":      int64(2),
				"end_line":        int64(4),
				"type_alias_kind": "mapped_type",
				"type_parameters": []any{"T"},
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"typescript","entity_type":"type_alias","query":"ReadonlyMap","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed type alias", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "TypeAlias ReadonlyMap is a mapped type and declares type parameters T."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "mapped_type_alias"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_JavaScriptMethodPrefersGraphPathAndUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":   "graph-js-method-1",
				"name":        "getTab",
				"labels":      []string{"Function"},
				"file_path":   "src/app.js",
				"repo_id":     "repo-1",
				"repo_name":   "repo-1",
				"language":    "javascript",
				"start_line":  int64(10),
				"end_line":    int64(24),
				"docstring":   "Returns the active tab.",
				"method_kind": "getter",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"javascript","entity_type":"function","query":"getTab","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed JavaScript function", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-js-method-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
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
	if got, want := profile["method_kind"], "getter"; got != want {
		t.Fatalf("semantic_profile[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("semantic_profile[docstring] = %#v, want %#v", got, want)
	}
	jsSemantics, ok := result["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[javascript_semantics] type = %T, want map[string]any", result["javascript_semantics"])
	}
	if got, want := jsSemantics["method_kind"], "getter"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := jsSemantics["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_PythonDecoratedAsyncFunctionUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-py-function-1",
				"name":       "handler",
				"labels":     []string{"Function"},
				"file_path":  "src/app.py",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "python",
				"start_line": int64(10),
				"end_line":   int64(20),
				"decorators": []string{"@route"},
				"async":      true,
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed Python function", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function handler is async and uses decorators @route."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_PythonLambdaFunctionUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":     "graph-py-lambda-1",
				"name":          "double",
				"labels":        []string{"Function"},
				"file_path":     "src/lambda.py",
				"repo_id":       "repo-1",
				"repo_name":     "repo-1",
				"language":      "python",
				"start_line":    int64(4),
				"end_line":      int64(4),
				"semantic_kind": "lambda",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"python","entity_type":"function","query":"double","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed Python lambda", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function double is a lambda function."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "lambda_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[python_semantics] type = %T, want map[string]any", result["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "lambda_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	signals, ok := pythonSemantics["signals"].([]any)
	if !ok {
		t.Fatalf("python_semantics[signals] type = %T, want []any", pythonSemantics["signals"])
	}
	if len(signals) != 1 || signals[0] != "lambda" {
		t.Fatalf("python_semantics[signals] = %#v, want [lambda]", signals)
	}
}

func TestHandleLanguageQuery_PythonDocumentedClassUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-py-class-1",
				"name":       "Logged",
				"labels":     []string{"Class"},
				"file_path":  "src/models.py",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "python",
				"start_line": int64(4),
				"end_line":   int64(8),
				"docstring":  "Represents a configured logger.",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"python","entity_type":"class","query":"Logged","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed Python class", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Class Logged is documented as \"Represents a configured logger.\"."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "documented_class"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["docstring"], "Represents a configured logger."; got != want {
		t.Fatalf("semantic_profile[docstring] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_ElixirGuardUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":     "graph-elixir-guard-1",
				"name":          "is_even",
				"labels":        []string{"Function"},
				"file_path":     "lib/demo/macros.ex",
				"repo_id":       "repo-1",
				"repo_name":     "repo-1",
				"language":      "elixir",
				"start_line":    int64(10),
				"end_line":      int64(10),
				"semantic_kind": "guard",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"guard","query":"is_even","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed Elixir guard", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function is_even is a guard."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "guard"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["guard"], true; got != want {
		t.Fatalf("semantic_profile[guard] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TSXComponentUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":              "graph-component-1",
				"name":                   "Screen",
				"labels":                 []any{"Component"},
				"file_path":              "src/Screen.tsx",
				"repo_id":                "repo-1",
				"repo_name":              "repo-1",
				"language":               "tsx",
				"start_line":             int64(7),
				"end_line":               int64(14),
				"framework":              "react",
				"jsx_fragment_shorthand": true,
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"tsx","entity_type":"component","query":"Screen","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed component", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Component Screen is associated with the react framework and uses JSX fragment shorthand."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "framework_component"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TypeScriptNamespaceUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":   "graph-ts-namespace-1",
				"name":        "API",
				"labels":      []string{"Module"},
				"file_path":   "src/types.ts",
				"repo_id":     "repo-1",
				"repo_name":   "repo-1",
				"language":    "typescript",
				"start_line":  int64(1),
				"end_line":    int64(8),
				"module_kind": "namespace",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"typescript","entity_type":"module","query":"API","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed namespace module", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Module API is a namespace."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "namespace_module"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TypeScriptDeclarationMergeUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":               "graph-ts-merge-1",
				"name":                    "Service",
				"labels":                  []string{"Class"},
				"file_path":               "src/merge.ts",
				"repo_id":                 "repo-1",
				"repo_name":               "repo-1",
				"language":                "typescript",
				"start_line":              int64(1),
				"end_line":                int64(6),
				"declaration_merge_group": "Service",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"class", "namespace"},
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"typescript","entity_type":"class","query":"Service","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed declaration-merge class", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "declaration_merge"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_TypeScriptClassFamilyUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		entityType   string
		wantLabel    string
		query        string
		row          map[string]any
		wantSummary  string
		wantSurface  string
		wantMetadata map[string]any
	}{
		{
			name:       "class decorators and generics",
			entityType: "class",
			wantLabel:  "Class",
			query:      "Demo",
			row: map[string]any{
				"entity_id":       "graph-ts-class-1",
				"name":            "Demo",
				"labels":          []any{"Class"},
				"file_path":       "src/decorators.ts",
				"repo_id":         "repo-1",
				"repo_name":       "repo-1",
				"language":        "typescript",
				"start_line":      int64(5),
				"end_line":        int64(20),
				"decorators":      []any{"@sealed"},
				"type_parameters": []any{"T"},
			},
			wantSummary: "Class Demo uses decorators @sealed and declares type parameters T.",
			wantSurface: "generic_declaration",
			wantMetadata: map[string]any{
				"decorators":      []any{"@sealed"},
				"type_parameters": []any{"T"},
			},
		},
		{
			name:       "interface declaration merge",
			entityType: "interface",
			wantLabel:  "Interface",
			query:      "Service",
			row: map[string]any{
				"entity_id":               "graph-ts-interface-1",
				"name":                    "Service",
				"labels":                  []any{"Interface"},
				"file_path":               "src/service.ts",
				"repo_id":                 "repo-1",
				"repo_name":               "repo-1",
				"language":                "typescript",
				"start_line":              int64(20),
				"end_line":                int64(32),
				"declaration_merge_group": "Service",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"class", "interface"},
			},
			wantSummary: "Interface Service participates in TypeScript declaration merging with class Service.",
			wantSurface: "declaration_merge",
			wantMetadata: map[string]any{
				"declaration_merge_group": "Service",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"class", "interface"},
			},
		},
		{
			name:       "enum declaration merge",
			entityType: "enum",
			wantLabel:  "Enum",
			query:      "ServiceKind",
			row: map[string]any{
				"entity_id":               "graph-ts-enum-1",
				"name":                    "ServiceKind",
				"labels":                  []any{"Enum"},
				"file_path":               "src/service.ts",
				"repo_id":                 "repo-1",
				"repo_name":               "repo-1",
				"language":                "typescript",
				"start_line":              int64(34),
				"end_line":                int64(42),
				"declaration_merge_group": "ServiceKind",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"enum", "namespace"},
			},
			wantSummary: "Enum ServiceKind participates in TypeScript declaration merging with namespace ServiceKind.",
			wantSurface: "declaration_merge",
			wantMetadata: map[string]any{
				"declaration_merge_group": "ServiceKind",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"enum", "namespace"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &LanguageQueryHandler{
				Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{tt.row}},
			}

			results, err := handler.queryByLanguageWithSemanticFilter(
				context.Background(),
				"typescript",
				tt.wantLabel,
				tt.query,
				"repo-1",
				10,
				"",
				"",
			)
			if err != nil {
				t.Fatalf("queryByLanguageWithSemanticFilter() error = %v, want nil", err)
			}
			if got, want := len(results), 1; got != want {
				t.Fatalf("len(results) = %d, want %d", got, want)
			}

			result := results[0]
			if got, want := result["semantic_summary"], tt.wantSummary; got != want {
				t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
			}

			profile, ok := result["semantic_profile"].(map[string]any)
			if !ok {
				t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
			}
			if got, want := profile["surface_kind"], tt.wantSurface; got != want {
				t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
			}

			metadata, ok := result["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
			}
			for key, want := range tt.wantMetadata {
				if got := metadata[key]; got == nil {
					t.Fatalf("metadata[%s] = nil, want %#v", key, want)
				}
			}
		})
	}
}

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

func TestHandleLanguageQuery_TSXVariableAssertionUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":                "graph-tsx-variable-1",
				"name":                     "Screen",
				"labels":                   []string{"Variable"},
				"file_path":                "src/Screen.tsx",
				"repo_id":                  "repo-1",
				"repo_name":                "repo-1",
				"language":                 "tsx",
				"start_line":               int64(3),
				"end_line":                 int64(3),
				"component_type_assertion": "ComponentType",
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
