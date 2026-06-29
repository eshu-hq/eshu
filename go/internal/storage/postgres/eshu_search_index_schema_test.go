// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
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
		"ON eshu_search_index_terms (scope_id, generation_id, term_key)",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing %q", want)
		}
	}
	if strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term, document_id)") {
		t.Fatal("eshu_search_index terms still key raw term text")
	}
}

// TestBootstrapDefinitionsEshuSearchIndexTermsHasDocumentIndex asserts that the
// standalone migration 037_eshu_search_index_terms_doc_idx.sql declares the
// document-keyed covering index eshu_search_index_terms_doc_idx on
// (scope_id, generation_id, document_id) using CREATE INDEX CONCURRENTLY so
// that bootstrap on a populated eshu_search_index_terms table does not take a
// blocking table-level lock during the index build.
//
// This index makes the per-page refresh DELETE (document_id = ANY) and the
// finalize retire DELETE (document_id <> ALL) seek directly to a document's
// rows instead of scanning the full (scope, generation) PK slice.
// A future migration must NOT remove this index or drop CONCURRENTLY without
// updating this test, keeping the index/query contract drift-proof.
func TestBootstrapDefinitionsEshuSearchIndexTermsHasDocumentIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_index_terms_doc_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("eshu_search_index_terms_doc_idx definition missing — " +
			"expected standalone migration 037_eshu_search_index_terms_doc_idx.sql")
	}
	const wantIndex = "ON eshu_search_index_terms (scope_id, generation_id, document_id)"
	if !strings.Contains(marker.SQL, wantIndex) {
		t.Fatalf("eshu_search_index_terms_doc_idx SQL missing document-keyed index %q\n"+
			"This index covers eshuSearchIndexRefreshDocumentTermsQuery (= ANY) and\n"+
			"eshuSearchIndexRetireTermsQuery (<> ALL).", wantIndex)
	}
	const wantConcurrently = "CONCURRENTLY"
	if !strings.Contains(marker.SQL, wantConcurrently) {
		t.Fatalf("eshu_search_index_terms_doc_idx SQL missing %q — "+
			"the migration must use CREATE INDEX CONCURRENTLY so bootstrap on a "+
			"populated table does not take a blocking lock during the index build.", wantConcurrently)
	}
}
