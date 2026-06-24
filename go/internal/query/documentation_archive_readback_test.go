// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"fmt"
	"testing"
)

func TestContentReaderDocumentationFactsReadsArchiveContainedFacts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		archivePath string
	}{
		{name: "zip", archivePath: "docs/support-packet.zip"},
		{name: "tar_gz", archivePath: "docs/support-packet.tar.gz"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			documentID := "doc:git:repository:r_archive:" + tc.archivePath + "!/runbook.md"
			row := []byte(fmt.Sprintf(`{
		"fact_id": "fact:archive:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "scope-archive",
		"generation_id": "gen-archive",
		"payload": {
			"document_id": %q,
			"section_id": "section:restore-service",
			"heading_text": "Restore Service",
			"content": "Follow the recovery checklist.",
			"content_format": "markdown",
			"source_metadata": {
				"path": %q,
				"archive_path": %q,
				"archive_member_path": "runbook.md",
				"archive_member_ordinal": "1",
				"archive_member_hash": "sha256:member"
			},
			"linked_entities": [{
				"entity_type": "repository",
				"entity_id": "repository:r_archive"
			}]
		}
	}`, documentID, tc.archivePath+"!/runbook.md", tc.archivePath))
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
				DocumentID: documentID,
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
			if got, want := metadata["archive_path"], tc.archivePath; got != want {
				t.Fatalf("archive_path = %#v, want %#v", got, want)
			}
			if got, want := metadata["archive_member_path"], "runbook.md"; got != want {
				t.Fatalf("archive_member_path = %#v, want %#v", got, want)
			}
		})
	}
}
