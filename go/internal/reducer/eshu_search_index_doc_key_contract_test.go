// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestEshuSearchIndexDeleteQueriesFilterOnDocumentID is a drift-proof contract
// test that asserts the two hot DELETE query constants — used on every
// per-page write (refresh) and every finalize retire — both filter on
// scope_id, generation_id, and document_id.
//
// Background (#4234): the table eshu_search_index_terms has PK
// (scope_id, generation_id, term_key, document_id) and a lookup index on
// (scope_id, generation_id, term_key). Neither structure supports seeking
// directly to a document's rows; both hot DELETEs were forced to scan the
// full (scope, generation) PK slice. The fix adds index
// eshu_search_index_terms_doc_idx on (scope_id, generation_id, document_id).
// This test ensures the query predicates remain aligned with that index:
// if someone rewrites the queries to drop document_id from the WHERE clause,
// this test fails immediately rather than silently regressing query plans.
func TestEshuSearchIndexDeleteQueriesFilterOnDocumentID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "eshuSearchIndexRefreshDocumentTermsQuery",
			query: eshuSearchIndexRefreshDocumentTermsQuery,
		},
		{
			name:  "eshuSearchIndexRetireTermsQuery",
			query: eshuSearchIndexRetireTermsQuery,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, fragment := range []string{"scope_id", "generation_id", "document_id"} {
				if !strings.Contains(tt.query, fragment) {
					t.Errorf("%s: query missing %q predicate — the document-keyed index\n"+
						"eshu_search_index_terms_doc_idx requires this filter to be effective;\n"+
						"removing it forces a full (scope,generation) slice scan on a 73M-row table.\n"+
						"Query:\n%s", tt.name, fragment, tt.query)
				}
			}
		})
	}
}

// TestEshuSearchIndexRefreshQueryUsesAnyPredicate verifies the refresh query
// uses the = ANY array form so the planner can leverage the document-keyed index
// (eshu_search_index_terms_doc_idx) with a single index scan over the listed IDs
// rather than one scan per document.
func TestEshuSearchIndexRefreshQueryUsesAnyPredicate(t *testing.T) {
	t.Parallel()

	if !strings.Contains(eshuSearchIndexRefreshDocumentTermsQuery, "ANY(") &&
		!strings.Contains(eshuSearchIndexRefreshDocumentTermsQuery, "= ANY(") &&
		!strings.Contains(eshuSearchIndexRefreshDocumentTermsQuery, "ANY($") {
		t.Errorf("eshuSearchIndexRefreshDocumentTermsQuery must use = ANY($N::text[]) predicate\n"+
			"so the planner seeks via eshu_search_index_terms_doc_idx.\n"+
			"Query:\n%s", eshuSearchIndexRefreshDocumentTermsQuery)
	}
}

// TestEshuSearchIndexRetireQueryUsesNotAllPredicate verifies the retire query
// uses the <> ALL array form. Both = ANY and <> ALL on (scope_id, generation_id,
// document_id) are covered by eshu_search_index_terms_doc_idx.
func TestEshuSearchIndexRetireQueryUsesNotAllPredicate(t *testing.T) {
	t.Parallel()

	if !strings.Contains(eshuSearchIndexRetireTermsQuery, "<> ALL(") &&
		!strings.Contains(eshuSearchIndexRetireTermsQuery, "<> ALL($") {
		t.Errorf("eshuSearchIndexRetireTermsQuery must use <> ALL($N::text[]) predicate\n"+
			"so the planner can leverage eshu_search_index_terms_doc_idx.\n"+
			"Query:\n%s", eshuSearchIndexRetireTermsQuery)
	}
}
