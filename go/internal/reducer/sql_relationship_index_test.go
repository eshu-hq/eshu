// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractSQLRelationshipRowsFromIndexOnTable proves a SqlIndex entity
// whose table_name metadata resolves to a present SqlTable yields exactly one
// INDEXES row, directed index -> table (#5330 Task 3).
func TestExtractSQLRelationshipRowsFromIndexOnTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_idx1",
				"entity_type": "SqlIndex",
				"entity_name": "users_email_idx",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlIndex",
				},
			},
		},
	}

	repoIDs, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_idx1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "INDEXES"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlIndex"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

// TestExtractSQLRelationshipRowsIndexUnresolvedTableIsSkipped proves a
// SqlIndex entity whose table_name does not resolve to any present SqlTable
// entity yields NO row — the extractor must skip, not fabricate, an edge to a
// table it cannot resolve (#5330 Task 3).
func TestExtractSQLRelationshipRowsIndexUnresolvedTableIsSkipped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_idx1",
				"entity_type": "SqlIndex",
				"entity_name": "users_email_idx",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlIndex",
				},
			},
		},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (unresolved table_name must not fabricate an edge): %#v", len(rows), rows)
	}
}

// TestExtractSQLRelationshipRowsIndexDeduplicates proves a duplicate INDEXES
// edge derived from re-seen SqlIndex content_entity facts (e.g. delta
// reprocessing re-emitting the same entity) collapses to one row via the
// entityID->INDEXES->targetID seenEdges key (#5330 Task 3).
func TestExtractSQLRelationshipRowsIndexDeduplicates(t *testing.T) {
	t.Parallel()

	indexPayload := map[string]any{
		"repo_id":     "repo-123",
		"entity_id":   "content-entity:e_idx1",
		"entity_type": "SqlIndex",
		"entity_name": "users_email_idx",
		"entity_metadata": map[string]any{
			"table_name":      "public.users",
			"sql_entity_type": "SqlIndex",
		},
	}
	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{FactKind: "content_entity", Payload: indexPayload},
		{FactKind: "content_entity", Payload: indexPayload},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (deduplication)", len(rows))
	}
}
