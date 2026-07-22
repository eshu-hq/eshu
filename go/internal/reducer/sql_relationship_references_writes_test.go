// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSQLRelationshipRowsMaterializesForeignKeyReferencesTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:orgs", "SqlTable", "public.orgs", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", map[string]any{
			"referenced_tables": []any{"public.orgs"},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	assertSQLRelationshipRow(t, rows[0], "content-entity:users", "SqlTable", "REFERENCES_TABLE", "content-entity:orgs", "SqlTable")
	if stats.UnresolvedReferenceTargets != 0 || stats.AmbiguousReferenceTargets != 0 {
		t.Fatalf("reference stats = %+v, want zero", stats)
	}
}

func TestExtractSQLRelationshipRowsMaterializesRoutineWritesTo(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:archive", "SqlTable", "public.user_archive", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:routine", "SqlFunction", "public.archive_users", "db/routines.sql", map[string]any{
			"write_tables": []any{"public.user_archive"},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	assertSQLRelationshipRow(t, rows[0], "content-entity:routine", "SqlFunction", "WRITES_TO", "content-entity:archive", "SqlTable")
	if stats.UnresolvedWriteTargets != 0 || stats.AmbiguousWriteTargets != 0 {
		t.Fatalf("write stats = %+v, want zero", stats)
	}
}

func TestExtractSQLRelationshipRowsRefusesAmbiguousAndMissingTableTargets(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipContentEntity("content-entity:orgs-a", "SqlTable", "public.orgs", "db/a.sql", nil),
		sqlRelationshipContentEntity("content-entity:orgs-b", "SqlTable", "public.orgs", "db/b.sql", nil),
		sqlRelationshipContentEntity("content-entity:users", "SqlTable", "public.users", "db/schema.sql", map[string]any{
			"referenced_tables": []any{"public.orgs", "public.missing"},
		}),
		sqlRelationshipContentEntity("content-entity:routine", "SqlFunction", "public.write_users", "db/routines.sql", map[string]any{
			"write_tables": []any{"public.orgs", "public.missing"},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want no guessed edges", rows)
	}
	if stats.AmbiguousReferenceTargets != 1 || stats.UnresolvedReferenceTargets != 1 ||
		stats.AmbiguousWriteTargets != 1 || stats.UnresolvedWriteTargets != 1 {
		t.Fatalf("stats = %+v, want one ambiguous and one unresolved target per family", stats)
	}
}

func assertSQLRelationshipRow(
	t *testing.T,
	row map[string]any,
	sourceID string,
	sourceType string,
	relationshipType string,
	targetID string,
	targetType string,
) {
	t.Helper()
	for key, want := range map[string]any{
		"source_entity_id":   sourceID,
		"source_entity_type": sourceType,
		"relationship_type":  relationshipType,
		"target_entity_id":   targetID,
		"target_entity_type": targetType,
	} {
		if got := row[key]; got != want {
			t.Errorf("row[%q] = %#v, want %#v: %#v", key, got, want, row)
		}
	}
}
