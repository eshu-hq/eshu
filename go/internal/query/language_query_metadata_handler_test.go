// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleLanguageQuery_SqlTableFallsBackToContentReportsContentIndexTruth
// and TestHandleLanguageQuery_GuardFallsBackToContentReportsContentIndexTruth
// below are the #5761 P1-3 review-fix regression:
// TestHandleLanguageQuery_AnnotationFallsBackToContentWhenGraphMissing above
// already exercises the "graph read returns zero rows, content store serves
// the whole answer" path for a graphFirstContentBackedEntityTypes member, but
// it asserts nothing about truth.basis, truth.level, or source_backend.
// language_queries.go's queryGraphFirstContentByLanguageWithSemanticFilter
// returns TruthBasisContentIndex on that exact path
// (language_queries.go:439); mutating that literal to TruthBasisHybrid left
// the whole package green because no test read the response's truth or
// source_backend fields on this path. These two tests cover both graph-first
// call sites that can take it: the graphFirstContentBackedEntityTypes
// dispatch branch (using "sql_table") and the separate "guard" special case
// (queryGraphFirstContentByLanguageWithSemanticFilter with a
// semantic_kind=guard filter) -- each is its own call site in
// handleLanguageQuery, so covering one does not prove the other.
func TestHandleLanguageQuery_SqlTableFallsBackToContentReportsContentIndexTruth(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "db/schema.sql", "SqlTable", "users",
					int64(1), int64(10), "sql", "CREATE TABLE users (...)", []byte(`{"schema":"public"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{}, // non-nil reader, zero rows: graph read serves nothing
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"sql","entity_type":"sql_table","query":"users","repo_id":"repo-1"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	assertLanguageQueryContentIndexTruthEnvelope(t, w.Body.Bytes(),
		"graph-first content-fallback read; the graph read returned no rows (or no graph reader was configured), so the content-store fallback served the result")
}

func TestHandleLanguageQuery_GuardFallsBackToContentReportsContentIndexTruth(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/guard.go", "Function", "IsValid",
					int64(1), int64(5), "go", "func IsValid() bool { ... }", []byte(`{"semantic_kind":"guard"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{}, // non-nil reader, zero rows: graph read serves nothing
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"go","entity_type":"guard","query":"IsValid","repo_id":"repo-1"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	assertLanguageQueryContentIndexTruthEnvelope(t, w.Body.Bytes(),
		"graph-first content-fallback read with a semantic_kind=guard filter; the graph read returned no rows (or no graph reader was configured), so the content-store fallback served the result")
}

// assertLanguageQueryContentIndexTruthEnvelope asserts the shared shape both
// graph-first-content-fallback tests above expect: an envelope response whose
// data.source_backend is "postgres_content_store", whose truth.basis/
// truth.level are "content_index"/"derived", and whose truth.reason matches
// wantReason exactly -- the honest basis and reason for "the graph read
// returned zero rows (or no graph reader was configured), so the
// content-store fallback served the result" (#5761 P1-3/P2-1). wantReason
// differs between call sites only by the "guard" branch's extra
// " with a semantic_kind=guard filter" note (languageQueryGraphFirstReason's
// filterNote parameter), so each caller passes its own exact expected text
// rather than sharing one constant.
func assertLanguageQueryContentIndexTruthEnvelope(t *testing.T, body []byte, wantReason string) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("resp[data] type = %T, want map[string]any (body=%s)", resp["data"], body)
	}
	if got, want := data["source_backend"], "postgres_content_store"; got != want {
		t.Fatalf("data[source_backend] = %#v, want %#v", got, want)
	}

	truth, ok := resp["truth"].(map[string]any)
	if !ok {
		t.Fatalf("resp[truth] type = %T, want map[string]any (body=%s)", resp["truth"], body)
	}
	if got, want := truth["basis"], "content_index"; got != want {
		t.Fatalf("truth[basis] = %#v, want %#v", got, want)
	}
	if got, want := truth["level"], "derived"; got != want {
		t.Fatalf("truth[level] = %#v, want %#v", got, want)
	}
	if got := truth["reason"]; got != wantReason {
		t.Fatalf("truth[reason] = %#v, want %#v", got, wantReason)
	}
}

// Handler-level metadata tests for POST /api/v0/code/language-query.
// Split out of language_query_metadata_test.go, which the #5761 truth-basis
// threading pushed past the repo's 500-line file cap. The enrichment unit
// tests stay in that file; these exercise the handler end to end.

func TestHandleLanguageQuery_RustImplBlockPrefersGraphPathAndEnrichesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
					int64(1), int64(18), "rust", "impl Display for Point {}", []byte(`{"kind":"trait_impl","trait":"Display","target":"Point"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Point",
				"labels":     []string{"ImplBlock"},
				"file_path":  "src/point.rs",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "rust",
				"start_line": int64(1),
				"end_line":   int64(18),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"rust","entity_type":"impl_block","query":"Point","repo_id":"repo-1"}`))
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
		t.Fatalf("results = %#v, want one graph-backed impl block", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "ImplBlock Point implements Display for Point."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["kind"], "trait_impl"; got != want {
		t.Fatalf("metadata[kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationPrefersGraphPathAndEnrichesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Logged",
				"labels":     []string{"Annotation"},
				"file_path":  "src/Logged.java",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "java",
				"start_line": int64(2),
				"end_line":   int64(2),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
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
		t.Fatalf("results = %#v, want one graph-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("result[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationFallsBackToContentWhenGraphMissing(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
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
		t.Fatalf("results = %#v, want one content-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "annotation-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":   "graph-annotation-1",
				"name":        "Logged",
				"labels":      []string{"Annotation"},
				"file_path":   "src/Logged.java",
				"repo_id":     "repo-1",
				"repo_name":   "repo-1",
				"language":    "java",
				"start_line":  int64(2),
				"end_line":    int64(2),
				"kind":        "applied",
				"target_kind": "method_declaration",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
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
		t.Fatalf("results = %#v, want one graph-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("result[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}
