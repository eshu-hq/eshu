package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestResolveEntityFallsBackToContentEntities(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"alias-1", "repo-1", "src/types.ts", "TypeAlias", "UserID",
					int64(3), int64(3), "typescript", "type UserID = string", []byte(`{"type":"string"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"UserID","type":"type_alias","repo_id":"repo-1"}`),
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
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["name"], "UserID"; got != want {
		t.Fatalf("entity[name] = %#v, want %#v", got, want)
	}
	if got, want := entity["language"], "typescript"; got != want {
		t.Fatalf("entity[language] = %#v, want %#v", got, want)
	}
	metadata, ok := entity["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity[metadata] type = %T, want map[string]any", entity["metadata"])
	}
	if got, want := metadata["type"], "string"; got != want {
		t.Fatalf("entity[metadata][type] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToAnyRepoContentMatchesAndAliases(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-2", "src/handler.py", "Function", "handler",
					int64(12), int64(20), "python", "async def handler(): ...", []byte(`{"decorators":["@route"],"async":true}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"handler","type":"function"}`),
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
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one cross-repo content-backed entity", resp["entities"])
	}
	matches, ok := resp["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want alias for one entity", resp["matches"])
	}
	if !reflect.DeepEqual(matches, entities) {
		t.Fatalf("matches = %#v, want alias of entities %#v", matches, entities)
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["repo_id"], "repo-2"; got != want {
		t.Fatalf("entity[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := entity["semantic_summary"], "Function handler is async and uses decorators @route."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityReturnsGraphBackedTypeScriptClassWithTypeScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["name"], "Service"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
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
						"id":                      "class-ts-1",
						"labels":                  []any{"Class"},
						"name":                    "Service",
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
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Service","type":"class","repo_id":"repo-1"}`),
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
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one graph-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	semantics, ok := entity["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[typescript_semantics] type = %T, want map[string]any", entity["typescript_semantics"])
	}
	if got, want := semantics["decorators"], []any{"@sealed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := semantics["type_parameters"], []any{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityReturnsGraphBackedJavaScriptFunctionWithJavaScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["name"], "getTab"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
				}
				if got, want := params["type"], "Function"; got != want {
					t.Fatalf("params[type] = %#v, want %#v", got, want)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.docstring as docstring",
					"e.method_kind as method_kind",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}
				return []map[string]any{
					{
						"id":          "function-js-1",
						"labels":      []any{"Function"},
						"name":        "getTab",
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
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"getTab","type":"function","repo_id":"repo-1"}`),
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
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one graph-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := entity["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity[metadata] type = %T, want map[string]any", entity["metadata"])
	}
	if got, want := metadata["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("entity[metadata][docstring] = %#v, want %#v", got, want)
	}
	if got, want := metadata["method_kind"], "getter"; got != want {
		t.Fatalf("entity[metadata][method_kind] = %#v, want %#v", got, want)
	}
	semantics, ok := entity["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[javascript_semantics] type = %T, want map[string]any", entity["javascript_semantics"])
	}
	if got, want := semantics["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
	if got, want := semantics["method_kind"], "getter"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "javascript_method"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextUsesSharedSemanticProjectionSeparatorContract(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "entity-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				if matched, err := regexp.MatchString(`deploy_entry_point as deploy_entry_point\s*r\.id as repo_id`, cypher); err != nil {
					t.Fatalf("regexp.MatchString() error = %v, want nil", err)
				} else if matched {
					t.Fatalf("cypher is missing the separator after the shared semantic projection: %q", cypher)
				}
				return map[string]any{
					"id":            "entity-1",
					"labels":        []any{"Function"},
					"name":          "handlePayment",
					"file_path":     "src/payments.ts",
					"repo_id":       "repo-1",
					"repo_name":     "payments",
					"language":      "typescript",
					"start_line":    int64(10),
					"end_line":      int64(32),
					"relationships": []any{},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-1/context", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestResolveEntityFallsBackToContentEntitiesWithSemanticSummary(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
					int64(1), int64(18), "rust", "impl Display for Point {}", []byte(`{"kind":"trait_impl","trait":"Display","target":"Point"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Point","type":"impl_block","repo_id":"repo-1"}`),
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
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["semantic_summary"], "ImplBlock Point implements Display for Point."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToContentEntities(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"component-1", "repo-1", "src/Button.tsx", "Component", "Button",
					int64(1), int64(12), "tsx", "export function Button() {}", []byte(`{"framework":"react"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/App.tsx", "Function", "renderApp",
					int64(5), int64(20), "tsx", "return <Button />", []byte(`{"jsx_component_usage":["Button"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/component-1/context", nil)
	req.SetPathValue("entity_id", "component-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["name"], "Button"; got != want {
		t.Fatalf("resp[name] = %#v, want %#v", got, want)
	}
	if got, want := resp["language"], "tsx"; got != want {
		t.Fatalf("resp[language] = %#v, want %#v", got, want)
	}
	metadata, ok := resp["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("resp[metadata] type = %T, want map[string]any", resp["metadata"])
	}
	if got, want := metadata["framework"], "react"; got != want {
		t.Fatalf("resp[metadata][framework] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(resp[relationships]) = %d, want 1", len(relationships))
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "REFERENCES"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_name"], "renderApp"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "jsx_component_usage"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
	if got, want := resp["semantic_summary"], "Component Button is associated with the react framework."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToContentRustImplBlockContext(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
					int64(1), int64(18), "rust", "impl Point { fn new() -> Self { Self {} } }", []byte(`{"kind":"trait_impl","trait":"Display","target":"Point"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"fn-new", "repo-1", "src/point.rs", "Function", "new",
					int64(3), int64(7), "rust", "fn new() -> Self { Self {} }", []byte(`{"impl_context":"Point"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/impl-1/context", nil)
	req.SetPathValue("entity_id", "impl-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "ImplBlock Point implements Display for Point."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(resp[relationships]) = %d, want 1", len(relationships))
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "CONTAINS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "new"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "rust_impl_context"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
