// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderDocumentationFactsReadsXLSXSheetFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:xlsx:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-xlsx",
		"generation_id": "gen-xlsx",
		"payload": {
			"document_id": "doc:git:repository:r_sheet:docs/service-inventory.xlsx",
			"section_id": "section:sheet:1",
			"heading_text": "Inventory",
			"content": "sheet: Inventory\nrows: 2\ncolumns: service, owner_email, dependency",
			"content_format": "xlsx",
			"source_metadata": {
				"path": "docs/service-inventory.xlsx",
				"table_kind": "xlsx_sheet",
				"row_count": "2",
				"column_count": "3",
				"formula_count": "1"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_sheet"
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
		Repository: "repository:r_sheet",
		DocumentID: "doc:git:repository:r_sheet:docs/service-inventory.xlsx",
		Query:      "Inventory",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["content_format"], "xlsx"; got != want {
		t.Fatalf("content_format = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["table_kind"], "xlsx_sheet"; got != want {
		t.Fatalf("table_kind = %#v, want %#v", got, want)
	}
}
