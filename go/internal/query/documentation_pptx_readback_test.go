// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderDocumentationFactsReadsPPTXSlideFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:pptx:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-pptx",
		"generation_id": "gen-pptx",
		"payload": {
			"document_id": "doc:git:repository:r_deck:docs/release-review.pptx",
			"section_id": "section:slide:1",
			"heading_text": "Release Review",
			"content": "slide: Release Review\nReview the rollback checklist before release.",
			"content_format": "pptx",
			"source_metadata": {
				"path": "docs/release-review.pptx",
				"slide_ordinal": "1",
				"table_count": "1"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_deck"
			}]
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{row}},
		queryContains: []string{
			"fact_records.payload @>",
			"fact_records.payload->>'document_id'",
			"LOWER(",
		},
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFacts(t.Context(), documentationFactFilter{
		FactKind:   "documentation_section",
		Repository: "repository:r_deck",
		DocumentID: "doc:git:repository:r_deck:docs/release-review.pptx",
		Query:      "Release",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["content_format"], "pptx"; got != want {
		t.Fatalf("content_format = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["slide_ordinal"], "1"; got != want {
		t.Fatalf("slide_ordinal = %#v, want %#v", got, want)
	}
}
