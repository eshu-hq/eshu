package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestResolveEntityReturnsGraphBackedPythonDecoratedClassWithPythonSemantics(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["name"], "Logged"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
				}
				if got, want := params["type"], "Class"; got != want {
					t.Fatalf("params[type] = %#v, want %#v", got, want)
				}
				if want := "e.decorators as decorators"; !strings.Contains(cypher, want) {
					t.Fatalf("cypher = %q, want %q", cypher, want)
				}
				if matched, err := regexp.MatchString(`,\s*,`, cypher); err != nil {
					t.Fatalf("regexp.MatchString() error = %v, want nil", err)
				} else if matched {
					t.Fatalf("cypher contains a duplicated projection separator: %q", cypher)
				}
				return []map[string]any{
					{
						"id":         "class-py-1",
						"labels":     []any{"Class"},
						"name":       "Logged",
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
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Logged","type":"class","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Class Logged is decorated with @tracked."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	decorators, ok := pythonSemantics["decorators"].([]any)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []any", pythonSemantics["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@tracked" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@tracked]", decorators)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_class"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToContentBackedPythonDecoratedAsyncFunction(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/handler.py", "Function", "handler",
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
		bytes.NewBufferString(`{"name":"handler","type":"function","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Function handler is async and uses decorators @route."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
	decorators, ok := pythonSemantics["decorators"].([]any)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []any", pythonSemantics["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@route]", decorators)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToContentBackedPythonAsyncFunction(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/worker.py", "Function", "run",
					int64(7), int64(15), "python", "async def run(): ...", []byte(`{"async":true}`),
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
		bytes.NewBufferString(`{"name":"run","type":"function","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Function run is async."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToContentBackedPythonDecoratedFunction(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"function-1", "repo-1", "src/handler.py", "Function", "handler",
					int64(12), int64(20), "python", "def handler(): ...", []byte(`{"decorators":["@route"]}`),
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
		bytes.NewBufferString(`{"name":"handler","type":"function","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Function handler uses decorators @route."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	decorators, ok := pythonSemantics["decorators"].([]any)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []any", pythonSemantics["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@route]", decorators)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}
