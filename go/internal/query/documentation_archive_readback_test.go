package query

import (
	"database/sql/driver"
	"testing"
)

func TestContentReaderDocumentationFactsReadsArchiveContainedFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:archive:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-archive",
		"generation_id": "gen-archive",
		"payload": {
			"document_id": "doc:git:repository:r_archive:docs/support-packet.zip!/runbook.md",
			"section_id": "section:restore-service",
			"heading_text": "Restore Service",
			"content": "Follow the recovery checklist.",
			"content_format": "markdown",
			"source_metadata": {
				"path": "docs/support-packet.zip!/runbook.md",
				"archive_path": "docs/support-packet.zip",
				"archive_member_path": "runbook.md",
				"archive_member_ordinal": "1",
				"archive_member_hash": "sha256:member"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_archive"
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
		Repository: "repository:r_archive",
		DocumentID: "doc:git:repository:r_archive:docs/support-packet.zip!/runbook.md",
		Query:      "Restore",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["archive_path"], "docs/support-packet.zip"; got != want {
		t.Fatalf("archive_path = %#v, want %#v", got, want)
	}
	if got, want := metadata["archive_member_path"], "runbook.md"; got != want {
		t.Fatalf("archive_member_path = %#v, want %#v", got, want)
	}
}
