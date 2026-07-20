// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSQLRelationshipRowsFromTriggerOnTable(t *testing.T) {
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
				"entity_id":   "content-entity:e_trig1",
				"entity_type": "SqlTrigger",
				"entity_name": "users_touch",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlTrigger",
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
	if got, want := rows[0]["source_entity_id"], "content-entity:e_trig1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "TRIGGERS"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlTrigger"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsFromTriggerExecutingFunction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_fn1",
				"entity_type": "SqlFunction",
				"entity_name": "public.touch_updated_at",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_trig1",
				"entity_type": "SqlTrigger",
				"entity_name": "users_touch",
				"entity_metadata": map[string]any{
					"function_name":   "public.touch_updated_at",
					"sql_entity_type": "SqlTrigger",
					"table_name":      "public.users",
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
	if got, want := rows[0]["source_entity_id"], "content-entity:e_trig1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_fn1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "EXECUTES"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlTrigger"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlFunction"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsResolvesUnqualifiedTriggerFunction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_fn1",
				"entity_type":   "SqlFunction",
				"entity_name":   "public.touch_updated_at",
				"relative_path": "db/functions.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_trig1",
				"entity_type":   "SqlTrigger",
				"entity_name":   "users_touch",
				"relative_path": "db/functions.sql",
				"entity_metadata": map[string]any{
					"function_name":   "touch_updated_at",
					"sql_entity_type": "SqlTrigger",
				},
			},
		},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_fn1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "EXECUTES"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsPrefersSameFileSQLFunction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_other_fn",
				"entity_type":   "SqlFunction",
				"entity_name":   "public.handle_new_user",
				"relative_path": "examples/other/migration.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_local_fn",
				"entity_type":   "SqlFunction",
				"entity_name":   "public.handle_new_user",
				"relative_path": "examples/current/migration.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_trig1",
				"entity_type":   "SqlTrigger",
				"entity_name":   "on_auth_user_created",
				"relative_path": "examples/current/migration.sql",
				"entity_metadata": map[string]any{
					"function_name":   "public.handle_new_user",
					"sql_entity_type": "SqlTrigger",
					"table_name":      "auth.users",
				},
			},
		},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_local_fn"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsSkipsAmbiguousCrossFileSQLFunction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_first_fn",
				"entity_type":   "SqlFunction",
				"entity_name":   "public.handle_new_user",
				"relative_path": "examples/first/migration.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_second_fn",
				"entity_type":   "SqlFunction",
				"entity_name":   "public.handle_new_user",
				"relative_path": "examples/second/migration.sql",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:e_trig1",
				"entity_type":   "SqlTrigger",
				"entity_name":   "on_auth_user_created",
				"relative_path": "examples/current/migration.sql",
				"entity_metadata": map[string]any{
					"function_name":   "public.handle_new_user",
					"sql_entity_type": "SqlTrigger",
				},
			},
		},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for ambiguous cross-file function", len(rows))
	}
}
