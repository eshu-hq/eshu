// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractAllCodeRelationshipRowsQuarantinesFileFromBothExtractors closes the
// codex review finding on PR #4753: a malformed "file" fact must be excluded
// from EVERY code-relationship extractor, not just the code-call path. Before
// the fix, extractCodeCallRowsWithIndex quarantined a file missing
// relative_path while extractPythonMetaclassRowsWithIndex (and buildCodeEntityIndex)
// still processed the SAME raw envelopes, so the malformed fact could be
// dead-lettered as input_invalid AND still emit a Python USES_METACLASS intent —
// the quarantine did not actually prevent all graph projection from the bad
// fact.
//
// partitionCodegraphFileFacts now runs once at the orchestrator
// (extractAllCodeRelationshipRowsWithIndex) and drops the malformed file from
// the valid set fed to the shared entity index, the code-call extractor, and
// the metaclass extractor. This asserts a file missing relative_path but
// carrying a valid metaclass class pair is quarantined once and produces ZERO
// metaclass rows, while a valid sibling file's metaclass pair still projects.
func TestExtractAllCodeRelationshipRowsQuarantinesFileFromBothExtractors(t *testing.T) {
	t.Parallel()

	// Malformed: missing relative_path, but its parsed_file_data carries a
	// complete metaclass class pair that WOULD emit a USES_METACLASS row if the
	// metaclass extractor saw it.
	malformed := facts.Envelope{
		FactID:   "malformed-metaclass-file",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id": "repo-meta",
			// relative_path intentionally absent -> quarantined.
			"parsed_file_data": map[string]any{
				"path": "/repo/bad.py",
				"classes": []any{
					map[string]any{"name": "BadMeta", "line_number": 1, "uid": "content-entity:bad-meta"},
					map[string]any{"name": "BadLogged", "line_number": 4, "uid": "content-entity:bad-logged", "metaclass": "BadMeta"},
				},
			},
		},
	}

	// Valid sibling: a complete metaclass class pair that MUST still project.
	valid := facts.Envelope{
		FactID:   "valid-metaclass-file",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-meta",
			"relative_path": "models.py",
			"parsed_file_data": map[string]any{
				"path": "/repo/models.py",
				"classes": []any{
					map[string]any{"name": "GoodMeta", "line_number": 1, "uid": "content-entity:good-meta"},
					map[string]any{"name": "GoodLogged", "line_number": 4, "uid": "content-entity:good-logged", "metaclass": "GoodMeta"},
				},
			},
		},
	}

	_, _, _, metaclassRows, _, quarantined := extractAllCodeRelationshipRowsWithIndex([]facts.Envelope{malformed, valid})

	// Exactly one quarantine, for the malformed file.
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 (the missing-relative_path file): %+v", len(quarantined), quarantined)
	}
	if quarantined[0].factID != "malformed-metaclass-file" || quarantined[0].field != "relative_path" {
		t.Fatalf("quarantined[0] = {factID:%q field:%q}, want {malformed-metaclass-file relative_path}", quarantined[0].factID, quarantined[0].field)
	}

	// The malformed file must NOT emit a metaclass row — the quarantine is
	// authoritative for the metaclass path too. The valid sibling's pair must
	// still project.
	foundBad := false
	foundGood := false
	for _, row := range metaclassRows {
		if row["source_entity_id"] == "content-entity:bad-logged" || row["target_entity_id"] == "content-entity:bad-meta" {
			foundBad = true
		}
		if row["source_entity_id"] == "content-entity:good-logged" && row["target_entity_id"] == "content-entity:good-meta" {
			foundGood = true
		}
	}
	if foundBad {
		t.Fatalf("metaclassRows = %#v, want NO row from the quarantined malformed file (the codex finding: quarantine must prevent metaclass projection too)", metaclassRows)
	}
	if !foundGood {
		t.Fatalf("metaclassRows = %#v, want the valid sibling's GoodLogged->GoodMeta USES_METACLASS row to still project", metaclassRows)
	}
}
