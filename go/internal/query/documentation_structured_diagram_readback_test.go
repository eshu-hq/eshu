// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderDocumentationFactsReadsStructuredDiagramFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:structured-diagram:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-structured-diagram",
		"generation_id": "gen-structured-diagram",
		"payload": {
			"document_id": "doc:git:repository:r_diagram:docs/architecture.svg",
			"section_id": "section:diagram",
			"heading_text": "architecture",
			"content": "Documentation Graph\nSVG Runbook",
			"content_format": "svg",
			"source_metadata": {
				"path": "docs/architecture.svg",
				"format_family": "diagram",
				"incident_media_source_class": "diagram_label",
				"diagram_format": "svg"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_diagram"
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
		Repository: "repository:r_diagram",
		DocumentID: "doc:git:repository:r_diagram:docs/architecture.svg",
		Query:      "SVG Runbook",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["content_format"], "svg"; got != want {
		t.Fatalf("content_format = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["diagram_format"], "svg"; got != want {
		t.Fatalf("diagram_format = %#v, want %#v", got, want)
	}
	if got, want := metadata["incident_media_source_class"], "diagram_label"; got != want {
		t.Fatalf("incident_media_source_class = %#v, want %#v", got, want)
	}
}
