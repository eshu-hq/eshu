// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsBoundEshuSearchIndexTermKeys(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_index" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("eshu_search_index definition missing")
	}
	for _, want := range []string{
		"term_key TEXT NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, term_key, document_id)",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing %q", want)
		}
	}
	if strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term, document_id)") {
		t.Fatal("eshu_search_index terms still key raw term text")
	}
}

func TestBootstrapDefinitionsAvoidRedundantSearchTermLookupIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_index" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("eshu_search_index definition missing")
	}
	if !strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term_key, document_id)") {
		t.Fatal("eshu_search_index terms primary key no longer covers term lookup prefix")
	}
	if strings.Contains(marker.SQL, "CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("eshu_search_index should not create redundant lookup index; "+
			"primary key prefix (scope_id, generation_id, term_key) covers BM25 term lookup:\n%s", marker.SQL)
	}
	if strings.Contains(marker.SQL, "DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("eshu_search_index should not drop lookup index non-concurrently; "+
			"038_drop_eshu_search_index_terms_lookup_idx owns the concurrent drop:\n%s", marker.SQL)
	}
}

func TestDataPlaneSearchIndexSchemaAvoidsRedundantTermLookupIndex(t *testing.T) {
	t.Parallel()

	schemaPath := filepath.Join("..", "..", "..", "..", "schema", "data-plane", "postgres", "003b_eshu_search_index.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read data-plane search-index schema: %v", err)
	}
	sql := string(schema)
	if strings.Contains(sql, "CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("data-plane schema should not create redundant lookup index; "+
			"the primary key prefix covers BM25 term lookup:\n%s", sql)
	}
	if strings.Contains(sql, "DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("data-plane schema should not drop lookup index non-concurrently:\n%s", sql)
	}
	for _, want := range []string{
		"content_hash TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT ''",
		"eshu_search_index_documents_vector_pending_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("data-plane search-index schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsDropRedundantSearchTermLookupIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_eshu_search_index_terms_lookup_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("drop_eshu_search_index_terms_lookup_idx definition missing")
	}
	const want = "DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_lookup_idx"
	if !strings.Contains(marker.SQL, want) {
		t.Fatalf("drop lookup-index migration missing %q:\n%s", want, marker.SQL)
	}
}

// TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex asserts that the
// write-amplifying document-keyed term index is removed once the reducer stops
// issuing per-page document-keyed term refresh deletes.
func TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_eshu_search_index_terms_doc_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("drop_eshu_search_index_terms_doc_idx definition missing")
	}
	const wantDrop = "DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_doc_idx"
	if !strings.Contains(marker.SQL, wantDrop) {
		t.Fatalf("drop doc-index migration missing %q:\n%s", wantDrop, marker.SQL)
	}
	const wantConcurrently = "CONCURRENTLY"
	if !strings.Contains(marker.SQL, wantConcurrently) {
		t.Fatalf("drop doc-index migration missing %q:\n%s", wantConcurrently, marker.SQL)
	}
}
