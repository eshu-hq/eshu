package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRelationshipsFallsBackToContentEntityReferences(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
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
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1"}`),
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
	outgoing, ok := resp["outgoing"].([]any)
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if len(outgoing) != 1 {
		t.Fatalf("len(resp[outgoing]) = %d, want 1", len(outgoing))
	}
	relationship, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := relationship["type"], "REFERENCES"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "Button"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "jsx_component_usage"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipsFallsBackToContentComponentInboundReferences(t *testing.T) {
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

	handler := &CodeHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"component-1"}`),
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
	incoming, ok := resp["incoming"].([]any)
	if !ok {
		t.Fatalf("resp[incoming] type = %T, want []any", resp["incoming"])
	}
	if len(incoming) != 1 {
		t.Fatalf("len(resp[incoming]) = %d, want 1", len(incoming))
	}
	relationship, ok := incoming[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[incoming][0] type = %T, want map[string]any", incoming[0])
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
}

func TestHandleRelationshipsFiltersContentFallbackByDirectionAndType(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
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
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1","direction":"outgoing","relationship_type":"REFERENCES"}`),
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
	outgoing, ok := resp["outgoing"].([]any)
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if len(outgoing) != 1 {
		t.Fatalf("len(resp[outgoing]) = %d, want 1", len(outgoing))
	}
	incoming, ok := resp["incoming"].([]any)
	if !ok {
		t.Fatalf("resp[incoming] type = %T, want []any", resp["incoming"])
	}
	if len(incoming) != 0 {
		t.Fatalf("len(resp[incoming]) = %d, want 0", len(incoming))
	}
	relationship, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := relationship["type"], "REFERENCES"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
}

func TestHandleRelationshipsRejectsInvalidDirection(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1","direction":"sideways"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleRelationshipsFallsBackToContentNameLookup(t *testing.T) {
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

	handler := &CodeHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"Button","direction":"incoming","relationship_type":"REFERENCES"}`),
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
	if got, want := resp["entity_id"], "component-1"; got != want {
		t.Fatalf("resp[entity_id] = %#v, want %#v", got, want)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if len(outgoing) != 0 {
		t.Fatalf("len(resp[outgoing]) = %d, want 0", len(outgoing))
	}
	incoming, ok := resp["incoming"].([]any)
	if !ok {
		t.Fatalf("resp[incoming] type = %T, want []any", resp["incoming"])
	}
	if len(incoming) != 1 {
		t.Fatalf("len(resp[incoming]) = %d, want 1", len(incoming))
	}
}

func TestHandleRelationshipsFallsBackToContentRustImplBlockOwnership(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
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
	})

	handler := &CodeHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"fn-new"}`),
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
	incoming, ok := resp["incoming"].([]any)
	if !ok {
		t.Fatalf("resp[incoming] type = %T, want []any", resp["incoming"])
	}
	if len(incoming) != 1 {
		t.Fatalf("len(resp[incoming]) = %d, want 1", len(incoming))
	}
	relationship, ok := incoming[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[incoming][0] type = %T, want map[string]any", incoming[0])
	}
	if got, want := relationship["type"], "CONTAINS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_name"], "Point"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "rust_impl_context"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
