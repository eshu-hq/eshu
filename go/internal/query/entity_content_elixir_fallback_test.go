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

type mockEntityGraphReader struct {
	rows []map[string]any
}

func (m *mockEntityGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return m.rows, nil
}

func (m *mockEntityGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if len(m.rows) == 0 {
		return nil, nil
	}
	return m.rows[0], nil
}

func TestResolveEntityFallsBackToElixirProtocolContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"protocol-1", "repo-1", "lib/demo/serializable.ex", "Protocol", "Demo.Serializable",
					int64(1), int64(3), "elixir", "defprotocol Demo.Serializable do\n  def serialize(data)\nend", []byte(`{"module_kind":"protocol"}`),
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
		bytes.NewBufferString(`{"name":"Demo.Serializable","type":"protocol","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Protocol Demo.Serializable is a protocol."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "protocol"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToElixirProtocolImplementationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "lib/demo/serializable.ex", "ProtocolImplementation", "Demo.Serializable",
					int64(1), int64(4), "elixir", "defimpl Demo.Serializable, for: Demo.Worker do\nend", []byte(`{"module_kind":"protocol_implementation","protocol":"Demo.Serializable","implemented_for":"Demo.Worker"}`),
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
		bytes.NewBufferString(`{"name":"Demo.Serializable","type":"protocol_implementation","repo_id":"repo-1"}`),
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
	labels, ok := entity["labels"].([]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("entity[labels] = %#v, want one label", entity["labels"])
	}
	if got, want := labels[0], "ProtocolImplementation"; got != want {
		t.Fatalf("entity[labels][0] = %#v, want %#v", got, want)
	}
	metadata, ok := entity["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity[metadata] type = %T, want map[string]any", entity["metadata"])
	}
	if got, want := metadata["module_kind"], "protocol_implementation"; got != want {
		t.Fatalf("entity[metadata][module_kind] = %#v, want %#v", got, want)
	}
	if got, want := metadata["protocol"], "Demo.Serializable"; got != want {
		t.Fatalf("entity[metadata][protocol] = %#v, want %#v", got, want)
	}
	if got, want := metadata["implemented_for"], "Demo.Worker"; got != want {
		t.Fatalf("entity[metadata][implemented_for] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToElixirGuardContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"guard-1", "repo-1", "lib/demo/macros.ex", "Function", "is_even",
					int64(10), int64(10), "elixir", "defguard is_even(value) when rem(value, 2) == 0", []byte(`{"semantic_kind":"guard"}`),
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
		bytes.NewBufferString(`{"name":"is_even","type":"guard","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "Function is_even is a guard."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "guard"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityUsesElixirGuardGraphEntity(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: &mockEntityGraphReader{rows: []map[string]any{
			{
				"id":            "guard-1",
				"labels":        []string{"Function"},
				"name":          "is_even",
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
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"is_even","type":"guard","repo_id":"repo-1"}`),
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
		t.Fatalf("entities = %#v, want one graph-backed guard entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["semantic_summary"], "Function is_even is a guard."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "guard"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToElixirProtocolContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"protocol-1", "repo-1", "lib/demo/serializable.ex", "Protocol", "Demo.Serializable",
					int64(1), int64(3), "elixir", "defprotocol Demo.Serializable do\n  def serialize(data)\nend", []byte(`{"module_kind":"protocol"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/protocol-1/context", nil)
	req.SetPathValue("entity_id", "protocol-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["name"], "Demo.Serializable"; got != want {
		t.Fatalf("resp[name] = %#v, want %#v", got, want)
	}
	if got, want := resp["semantic_summary"], "Protocol Demo.Serializable is a protocol."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "protocol"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToElixirModuleAttributeContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"attr-1", "repo-1", "lib/demo/worker.ex", "Variable", "@timeout",
					int64(2), int64(2), "elixir", "@timeout 5_000", []byte(`{"attribute_kind":"module_attribute","value":"5_000"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/attr-1/context", nil)
	req.SetPathValue("entity_id", "attr-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "Variable @timeout is a module attribute."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "module_attribute"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	metadata, ok := resp["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("resp[metadata] type = %T, want map[string]any", resp["metadata"])
	}
	if got, want := metadata["attribute_kind"], "module_attribute"; got != want {
		t.Fatalf("resp[metadata][attribute_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToElixirProtocolImplementationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "lib/demo/serializable.ex", "ProtocolImplementation", "Demo.Serializable",
					int64(1), int64(4), "elixir", "defimpl Demo.Serializable, for: Demo.Worker do\nend", []byte(`{"module_kind":"protocol_implementation","protocol":"Demo.Serializable","implemented_for":"Demo.Worker"}`),
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
	if got, want := resp["semantic_summary"], "ProtocolImplementation Demo.Serializable is a protocol implementation for Demo.Worker via Demo.Serializable."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	labels, ok := resp["labels"].([]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("resp[labels] = %#v, want one label", resp["labels"])
	}
	if got, want := labels[0], "ProtocolImplementation"; got != want {
		t.Fatalf("resp[labels][0] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "protocol_implementation"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}
