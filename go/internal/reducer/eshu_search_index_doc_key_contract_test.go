// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestEshuSearchIndexTermClearUsesGenerationPrefix is a drift-proof contract
// test for the search-term write lifecycle. The writer clears the generation's
// term rows once before streaming refreshed pages, so the DELETE must stay
// scoped to the primary-key prefix (scope_id, generation_id) and must not depend
// on a document-keyed secondary index.
func TestEshuSearchIndexTermClearUsesGenerationPrefix(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{"scope_id", "generation_id"} {
		if !strings.Contains(eshuSearchIndexClearGenerationTermsQuery, fragment) {
			t.Errorf("eshuSearchIndexClearGenerationTermsQuery missing %q predicate:\n%s",
				fragment, eshuSearchIndexClearGenerationTermsQuery)
		}
	}
	if strings.Contains(eshuSearchIndexClearGenerationTermsQuery, "document_id") {
		t.Fatalf("generation term clear must not use document_id or require eshu_search_index_terms_doc_idx:\n%s",
			eshuSearchIndexClearGenerationTermsQuery)
	}
}

// TestEshuSearchIndexRetireDocumentsNoLongerRetiresTerms verifies finalize no
// longer needs a document-keyed term retire. Generation-scoped term cleanup
// happens once at stream start, avoiding per-page or finalize dependence on the
// write-amplifying eshu_search_index_terms_doc_idx index.
func TestEshuSearchIndexRetireDocumentsNoLongerRetiresTerms(t *testing.T) {
	t.Parallel()

	if strings.Contains(eshuSearchIndexRetireDocumentsQuery, "eshu_search_index_terms") {
		t.Fatalf("document retire query must not retire terms:\n%s", eshuSearchIndexRetireDocumentsQuery)
	}
}
