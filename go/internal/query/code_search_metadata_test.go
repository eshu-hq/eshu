package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEnrichGraphSearchResultsWithContentMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/handler.py", "Function", "handler",
					int64(12), int64(20), "python", "async def handler(): ...", []byte(`{"decorators":["@route"],"async":true}`),
				},
			},
		},
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   20,
		},
	}

	got, err := handler.enrichGraphSearchResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"repo-1",
		"handler",
		10,
	)
	if err != nil {
		t.Fatalf("enrichGraphSearchResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	if gotValue, want := metadata["async"], true; gotValue != want {
		t.Fatalf("metadata[async] = %#v, want %#v", gotValue, want)
	}
	decorators, ok := metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []any", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("metadata[decorators] = %#v, want [@route]", decorators)
	}
	if gotValue, want := got[0]["semantic_summary"], "Function handler is async and uses decorators @route."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "decorated_async_function"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["async"], true; gotValue != want {
		t.Fatalf("semantic_profile[async] = %#v, want %#v", gotValue, want)
	}
	decoratorValues, ok := semanticProfile["decorators"].([]string)
	if !ok {
		t.Fatalf("semantic_profile[decorators] type = %T, want []string", semanticProfile["decorators"])
	}
	if len(decoratorValues) != 1 || decoratorValues[0] != "@route" {
		t.Fatalf("semantic_profile[decorators] = %#v, want [@route]", decoratorValues)
	}
}

func TestEnrichGraphSearchResultsWithContentMetadataPrefersExistingJavaScriptMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-js-1",
			"name":       "getTab",
			"labels":     []string{"Function"},
			"file_path":  "src/app.js",
			"repo_id":    "repo-1",
			"language":   "javascript",
			"start_line": 10,
			"end_line":   24,
			"metadata": map[string]any{
				"docstring":   "Returns the active tab.",
				"method_kind": "getter",
			},
		},
	}

	got, err := handler.enrichGraphSearchResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"repo-1",
		"getTab",
		10,
	)
	if err != nil {
		t.Fatalf("enrichGraphSearchResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "javascript_method"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	jsSemantics, ok := got[0]["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][javascript_semantics] type = %T, want map[string]any", got[0]["javascript_semantics"])
	}
	if gotValue, want := jsSemantics["method_kind"], "getter"; gotValue != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := jsSemantics["docstring"], "Returns the active tab."; gotValue != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichGraphResultsWithContentMetadataByEntityIDPreservesPythonGraphMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"func:py:handler", "repo-1", "src/app.py", "Function", "handler",
					int64(10), int64(24), "python", "async def handler(): ...",
					[]byte(`{"docstring":"Handles incoming requests.","decorators":["@content"],"async":false}`),
				},
			},
		},
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"entity_id":  "func:py:handler",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/app.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 10,
			"end_line":   24,
			"metadata": map[string]any{
				"decorators": []string{"@route"},
				"async":      true,
			},
		},
	}

	got, err := handler.enrichGraphResultsWithContentMetadataByEntityID(context.Background(), results)
	if err != nil {
		t.Fatalf("enrichGraphResultsWithContentMetadataByEntityID() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	decorators, ok := metadata["decorators"].([]string)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []string", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("metadata[decorators] = %#v, want [@route]", decorators)
	}
	if gotValue, want := metadata["async"], true; gotValue != want {
		t.Fatalf("metadata[async] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["docstring"], "Handles incoming requests."; gotValue != want {
		t.Fatalf("metadata[docstring] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := got[0]["semantic_summary"], "Function handler is async, uses decorators @route, and is documented as \"Handles incoming requests.\"."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "decorated_async_function"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
}

func TestHandleSearchReturnsGraphBackedTypeScriptClassWithTypeScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["query"], "Service"; got != want {
					t.Fatalf("params[query] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.type_parameters as type_parameters",
					"e.declaration_merge_group as declaration_merge_group",
					"e.declaration_merge_count as declaration_merge_count",
					"e.declaration_merge_kinds as declaration_merge_kinds",
					"e.decorators as decorators",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
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

	results, err := handler.searchGraphEntities(context.Background(), "repo-1", "Service", "typescript", 10)
	if err != nil {
		t.Fatalf("searchGraphEntities() error = %v, want nil", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}

	result := results[0]
	if got, want := result["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	semantics, ok := result["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[typescript_semantics] type = %T, want map[string]any", result["typescript_semantics"])
	}
	if got, want := semantics["type_parameters"], []string{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
	if got, want := semantics["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("typescript_semantics[declaration_merge_group] = %#v, want %#v", got, want)
	}
}

func TestHandleSearchReturnsGraphBackedPythonDecoratedClassWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["query"], "Logged"; got != want {
					t.Fatalf("params[query] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id":  "class:py:logged",
						"name":       "Logged",
						"labels":     []any{"Class"},
						"file_path":  "src/models.py",
						"repo_id":    "repo-1",
						"repo_name":  "repo-1",
						"language":   "python",
						"start_line": int64(4),
						"end_line":   int64(8),
						"decorators": []any{"@tracked"},
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Logged","repo_id":"repo-1","language":"python"}`),
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
	if got, want := resp["source"], "graph"; got != want {
		t.Fatalf("resp[source] = %#v, want %#v", got, want)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed Python result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Class Logged is decorated with @tracked."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("result[python_semantics] type = %T, want map[string]any", result["python_semantics"])
	}
	decorators, ok := pythonSemantics["decorators"].([]any)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []any", pythonSemantics["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@tracked" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@tracked]", decorators)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_class"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestSearchGraphEntitiesDoesNotDuplicateRepoNameProjection(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := strings.Count(cypher, " as repo_name"), 1; got != want {
					t.Fatalf("strings.Count(cypher, \" as repo_name\") = %d, want %d; cypher=%q", got, want, cypher)
				}
				if strings.Contains(cypher, "e.repo_name as repo_name") {
					t.Fatalf("cypher = %q, must not alias entity repo_name onto the canonical repo_name column", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{}, nil
			},
		},
	}

	results, err := handler.searchGraphEntities(context.Background(), "repo-1", "Service", "typescript", 10)
	if err != nil {
		t.Fatalf("searchGraphEntities() error = %v, want nil", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestHandleSearchAllowsCrossRepoQueriesWhenRepoScopeIsOmitted(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if _, ok := params["repo_id"]; ok {
					t.Fatalf("params[repo_id] = %#v, want repo_id omitted for cross-repo search", params["repo_id"])
				}
				if strings.Contains(cypher, "r.id = $repo_id") {
					t.Fatalf("cypher = %q, want no repo filter when repo scope is omitted", cypher)
				}
				return []map[string]any{
					{
						"entity_id":  "func:ts:search",
						"name":       "search",
						"labels":     []any{"Function"},
						"file_path":  "src/search.ts",
						"repo_id":    "repo-2",
						"repo_name":  "repo-2",
						"language":   "typescript",
						"start_line": int64(4),
						"end_line":   int64(18),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"search","language":"typescript"}`),
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
	if got, want := resp["source"], "graph"; got != want {
		t.Fatalf("resp[source] = %#v, want %#v", got, want)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("resp[results] = %#v, want one graph-backed result", resp["results"])
	}
	matches, ok := resp["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("resp[matches] = %#v, want one compatibility alias result", resp["matches"])
	}
	if !reflect.DeepEqual(matches, results) {
		t.Fatalf("resp[matches] = %#v, want alias of resp[results] %#v", matches, results)
	}
	if got, want := resp["source_backend"], "graph"; got != want {
		t.Fatalf("resp[source_backend] = %#v, want %#v", got, want)
	}
}

func TestEnrichGraphSearchResultsWithContentMetadataSkipsUnmatchedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/other.py", "Function", "other",
					int64(1), int64(5), "python", "def other(): pass", []byte(`{"decorators":["@cached"]}`),
				},
			},
		},
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   20,
		},
	}

	got, err := handler.enrichGraphSearchResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"repo-1",
		"handler",
		10,
	)
	if err != nil {
		t.Fatalf("enrichGraphSearchResultsWithContentMetadata() error = %v, want nil", err)
	}
	if _, ok := got[0]["metadata"]; ok {
		t.Fatalf("results[0][metadata] = %#v, want metadata to remain absent", got[0]["metadata"])
	}
	if _, ok := got[0]["semantic_summary"]; ok {
		t.Fatalf("results[0][semantic_summary] = %#v, want semantic summary to remain absent", got[0]["semantic_summary"])
	}
	if _, ok := got[0]["semantic_profile"]; ok {
		t.Fatalf("results[0][semantic_profile] = %#v, want semantic profile to remain absent", got[0]["semantic_profile"])
	}
}

func TestHandleSearchReturnsGraphBackedJavaScriptMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["query"], "getTab"; got != want {
					t.Fatalf("params[query] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id":   "graph-js-1",
						"name":        "getTab",
						"labels":      []any{"Function"},
						"file_path":   "src/app.js",
						"repo_id":     "repo-1",
						"repo_name":   "repo-1",
						"language":    "javascript",
						"start_line":  int64(10),
						"end_line":    int64(24),
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
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"getTab","repo_id":"repo-1","language":"javascript"}`),
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
	if got, want := resp["source"], "graph"; got != want {
		t.Fatalf("resp[source] = %#v, want %#v", got, want)
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
}

func TestHandleSearchReturnsGraphBackedPythonTypeAnnotationsWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["query"], "greet"; got != want {
					t.Fatalf("params[query] = %#v, want %#v", got, want)
				}
				if want := "e.type_annotation_count as type_annotation_count"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				if want := "e.type_annotation_kinds as type_annotation_kinds"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				return []map[string]any{
					{
						"entity_id":             "func:py:greet",
						"name":                  "greet",
						"labels":                []any{"Function"},
						"file_path":             "src/app.py",
						"repo_id":               "repo-1",
						"repo_name":             "repo-1",
						"language":              "python",
						"start_line":            int64(10),
						"end_line":              int64(24),
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
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"greet","repo_id":"repo-1","language":"python"}`),
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
	if got, want := resp["source"], "graph"; got != want {
		t.Fatalf("resp[source] = %#v, want %#v", got, want)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed Python result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Function greet has parameter and return type annotations."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, ok := profile["type_annotation_count"].(float64); !ok || int(got) != 2 {
		t.Fatalf("semantic_profile[type_annotation_count] = %#v, want 2", profile["type_annotation_count"])
	}
}
