// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEnrichLanguageResultsWithContentMetadataPromotesExistingPythonSemanticsWithoutContent,
// TestEnrichLanguageResultsWithContentMetadataRustImplBlock, and
// TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata
// moved to language_query_metadata_promotion_test.go (#6642), rewritten onto
// the mounted route so they travel with the language family.

type mockLanguageQueryGraphReader struct {
	rows []map[string]any
}

func (m *mockLanguageQueryGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return m.rows, nil
}

func (m *mockLanguageQueryGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if len(m.rows) == 0 {
		return nil, nil
	}
	return m.rows[0], nil
}

// languageQueryMetadataResult drives a language-query request through the
// mounted route (#6642) and returns the decoded results[0] entry, so the
// tests in this file assert on the same wire shape a caller sees rather than
// calling the unexported enrichLanguageResultsWithContentMetadata method
// directly.
func languageQueryMetadataResult(t *testing.T, handler *LanguageQueryHandler, body string) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want exactly one result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	return result
}

func TestEnrichLanguageResultsWithContentMetadata(t *testing.T) {
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

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "handler",
				"labels":     []string{"Function"},
				"file_path":  "src/handler.py",
				"repo_id":    "repo-1",
				"language":   "python",
				"start_line": int64(12),
				"end_line":   int64(20),
			},
		}},
		Content: NewContentReader(db),
	}
	result := languageQueryMetadataResult(t, handler,
		`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`)

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
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
	if gotValue, want := result["semantic_summary"], "Function handler is async and uses decorators @route."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "decorated_async_function"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["async"], true; gotValue != want {
		t.Fatalf("semantic_profile[async] = %#v, want %#v", gotValue, want)
	}
	decoratorValues, ok := semanticProfile["decorators"].([]any)
	if !ok {
		t.Fatalf("semantic_profile[decorators] type = %T, want []any", semanticProfile["decorators"])
	}
	if len(decoratorValues) != 1 || decoratorValues[0] != "@route" {
		t.Fatalf("semantic_profile[decorators] = %#v, want [@route]", decoratorValues)
	}
}

// TestEnrichLanguageResultsWithContentMetadataSkipsUnmatchedRows covers the
// merged=false boundary: the content row and graph row never overlap, so the
// route must report TruthBasisAuthoritativeGraph (not TruthBasisHybrid) and
// the result must carry no metadata/semantic_summary/semantic_profile at all
// (#5761 P2-1). Driven through the mounted route (#6642): the truth
// envelope's basis is the same observable proof the direct merged-return-value
// assertion was.
func TestEnrichLanguageResultsWithContentMetadataSkipsUnmatchedRows(t *testing.T) {
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

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "handler",
				"labels":     []string{"Function"},
				"file_path":  "src/handler.py",
				"repo_id":    "repo-1",
				"language":   "python",
				"start_line": int64(12),
				"end_line":   int64(20),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth.basis = %q, want %q (unmatched content rows must not merge)", envelope.Truth.Basis, TruthBasisAuthoritativeGraph)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any (body = %s)", envelope.Data, rec.Body.String())
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("data.results = %#v, want exactly the one graph hit (an unmatched content row must not drop it)", data["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["name"], "handler"; got != want {
		t.Fatalf("results[0].name = %#v, want %q (the graph hit must survive unmatched enrichment)", got, want)
	}
	if got, want := result["file_path"], "src/handler.py"; got != want {
		t.Fatalf("results[0].file_path = %#v, want %q", got, want)
	}
	for _, absent := range []string{"metadata", "semantic_summary", "semantic_profile"} {
		if v, ok := result[absent]; ok {
			t.Fatalf("results[0][%s] = %#v, want key absent (unmatched content rows must not merge)", absent, v)
		}
	}
}

func TestEnrichLanguageResultsWithContentMetadataAnnotation(t *testing.T) {
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
				"language":   "java",
				"start_line": int64(2),
				"end_line":   int64(2),
			},
		}},
		Content: NewContentReader(db),
	}
	result := languageQueryMetadataResult(t, handler,
		`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`)

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
	}
	if gotValue, want := metadata["kind"], "applied"; gotValue != want {
		t.Fatalf("metadata[kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "applied_annotation"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
}
