// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

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
