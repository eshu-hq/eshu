// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderDocumentationFactsReadsDOCXSectionFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:docx:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-docx",
		"generation_id": "gen-docx",
		"payload": {
			"document_id": "doc:git:repository:r_docx:docs/migration-plan.docx",
			"section_id": "section:migration-plan",
			"heading_text": "Migration Plan",
			"content": "Review the bounded rollout sequence before release.",
			"content_format": "docx",
			"source_metadata": {
				"path": "docs/migration-plan.docx",
				"paragraph_count": "1",
				"table_count": "1"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_docx"
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
		Repository: "repository:r_docx",
		DocumentID: "doc:git:repository:r_docx:docs/migration-plan.docx",
		Query:      "Migration",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["content_format"], "docx"; got != want {
		t.Fatalf("content_format = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["table_count"], "1"; got != want {
		t.Fatalf("table_count = %#v, want %#v", got, want)
	}
}
