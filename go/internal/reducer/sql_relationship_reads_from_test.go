// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSQLRelationshipRowsFromViewReadingTable(t *testing.T) {
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
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.active_users",
				"entity_metadata": map[string]any{
					"source_tables":   []any{"public.users"},
					"sql_entity_type": "SqlView",
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
	if got, want := rows[0]["source_entity_id"], "content-entity:e_view1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "READS_FROM"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlView"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["repo_id"], "repo-123"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}
}

// TestExtractSQLRelationshipRowsViewOnViewReadsFromView guards #5345: a view
// whose source_tables names another view (not a table) must resolve the
// READS_FROM edge directly to that view, since resolveSQLReadTarget tries
// SqlTable first, then SqlView.
func TestExtractSQLRelationshipRowsViewOnViewReadsFromView(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.base_view",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view2",
				"entity_type": "SqlView",
				"entity_name": "public.derived_view",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.base_view"},
				},
			},
		},
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_view2"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_view1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlView"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "READS_FROM"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if stats.UnresolvedReadTargets != 0 || stats.AmbiguousReadTargets != 0 {
		t.Fatalf("stats = %+v, want zero", stats)
	}
}

// TestExtractSQLRelationshipRowsAmbiguousReadTargetSkipped guards #5345: when a
// source_tables name matches both a SqlTable and a SqlView in the same repo,
// the resolver must refuse to guess and skip the edge, tallying it ambiguous
// rather than silently picking one.
func TestExtractSQLRelationshipRowsAmbiguousReadTargetSkipped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.metrics",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.metrics",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_fn1",
				"entity_type": "SqlFunction",
				"entity_name": "public.summarize",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.metrics"},
				},
			},
		},
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (ambiguous target must be skipped): %#v", len(rows), rows)
	}
	if stats.AmbiguousReadTargets != 1 {
		t.Fatalf("stats.AmbiguousReadTargets = %d, want 1", stats.AmbiguousReadTargets)
	}
	if stats.UnresolvedReadTargets != 0 {
		t.Fatalf("stats.UnresolvedReadTargets = %d, want 0", stats.UnresolvedReadTargets)
	}
}

// TestExtractSQLRelationshipRowsReadTargetResolvesViaUnqualifiedFallback
// guards #5345: a qualified mention (e.g. "public.orders") must still resolve
// against a bare definition (e.g. "orders") on a full-name lookup miss.
func TestExtractSQLRelationshipRowsReadTargetResolvesViaUnqualifiedFallback(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "orders",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.order_summary",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.orders"},
				},
			},
		},
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "READS_FROM"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if stats.UnresolvedReadTargets != 0 || stats.AmbiguousReadTargets != 0 {
		t.Fatalf("stats = %+v, want zero", stats)
	}
}

// TestExtractSQLRelationshipRowsUnresolvedReadTargetTallied guards #5345: a
// source_tables name matching nothing in-repo (even after the unqualified
// fallback) must be skipped and tallied, not fabricated or dropped silently.
func TestExtractSQLRelationshipRowsUnresolvedReadTargetTallied(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.orphan_view",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.does_not_exist"},
				},
			},
		},
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: %#v", len(rows), rows)
	}
	if stats.UnresolvedReadTargets != 1 {
		t.Fatalf("stats.UnresolvedReadTargets = %d, want 1", stats.UnresolvedReadTargets)
	}
	if stats.AmbiguousReadTargets != 0 {
		t.Fatalf("stats.AmbiguousReadTargets = %d, want 0", stats.AmbiguousReadTargets)
	}
}

func TestExtractSQLRelationshipRowsDeduplicates(t *testing.T) {
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
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.active_users",
				"entity_metadata": map[string]any{
					"source_tables":   []any{"public.users", "public.users"},
					"sql_entity_type": "SqlView",
				},
			},
		},
	}

	_, rows, _ := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (deduplication)", len(rows))
	}
}
