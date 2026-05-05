package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLanguageQuery_ElixirProtocolUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":   "graph-protocol-1",
				"name":        "Demo.Serializable",
				"labels":      []string{"Protocol"},
				"file_path":   "lib/demo/serializable.ex",
				"repo_id":     "repo-1",
				"repo_name":   "repo-1",
				"language":    "elixir",
				"start_line":  int64(1),
				"end_line":    int64(3),
				"module_kind": "protocol",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"protocol","query":"Demo.Serializable","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed protocol", resp["results"])
	}
	result := results[0].(map[string]any)
	if got, want := result["semantic_summary"], "Protocol Demo.Serializable is a protocol."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile := result["semantic_profile"].(map[string]any)
	if got, want := profile["surface_kind"], "protocol"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_ElixirProtocolImplementationUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":       "graph-impl-1",
				"name":            "Demo.Serializable",
				"labels":          []string{"ProtocolImplementation"},
				"file_path":       "lib/demo/serializable.ex",
				"repo_id":         "repo-1",
				"repo_name":       "repo-1",
				"language":        "elixir",
				"start_line":      int64(5),
				"end_line":        int64(8),
				"module_kind":     "protocol_implementation",
				"protocol":        "Demo.Serializable",
				"implemented_for": "Demo.Worker",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"protocol_implementation","query":"Demo.Serializable","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed protocol implementation", resp["results"])
	}
	result := results[0].(map[string]any)
	if got, want := result["semantic_summary"], "ProtocolImplementation Demo.Serializable is a protocol implementation for Demo.Worker via Demo.Serializable."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile := result["semantic_profile"].(map[string]any)
	if got, want := profile["surface_kind"], "protocol_implementation"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_ElixirModuleAttributeUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":      "graph-attr-1",
				"name":           "@timeout",
				"labels":         []string{"Variable"},
				"file_path":      "lib/demo/worker.ex",
				"repo_id":        "repo-1",
				"repo_name":      "repo-1",
				"language":       "elixir",
				"start_line":     int64(2),
				"end_line":       int64(2),
				"attribute_kind": "module_attribute",
				"value":          "5_000",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"module_attribute","query":"@timeout","repo_id":"repo-1"}`),
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
		t.Fatalf("results = %#v, want one graph-backed module attribute", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Variable @timeout is a module attribute."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "module_attribute"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["attribute_kind"], "module_attribute"; got != want {
		t.Fatalf("metadata[attribute_kind] = %#v, want %#v", got, want)
	}
	if got, want := metadata["value"], "5_000"; got != want {
		t.Fatalf("metadata[value] = %#v, want %#v", got, want)
	}
}
