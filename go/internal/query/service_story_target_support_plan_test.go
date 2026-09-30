// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestServiceStoryTargetSupportSQLProbesFactKindIndex guards #6794. Joining
// fact_records to the active scope/generation pairs made Postgres estimate one
// fact per (scope_id, generation_id) pair (thousands in reality; extended
// statistics do not apply to join clauses) and scan every fact of every active
// generation through fact_records_scope_generation_keyset_idx, filtering the
// support kinds in the heap: 6.5s and 3.5M buffers per statement on a
// production-scale instance. Probing one (scope, generation, kind) triple at a
// time through a LATERAL subquery lets fact_records_scope_generation_idx
// (scope_id, generation_id, fact_kind, ...) answer with the kind in the index
// condition. OFFSET 0 is load-bearing: without it the planner flattens the
// LATERAL back into the join.
func TestServiceStoryTargetSupportSQLProbesFactKindIndex(t *testing.T) {
	t.Parallel()

	sourceOnlySQL, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for _, want := range []string{
		"CROSS JOIN (SELECT DISTINCT unnest($1::text[]) AS fact_kind) AS kind",
		"CROSS JOIN LATERAL (",
		"fact.scope_id = scope.scope_id",
		"fact.generation_id = scope.active_generation_id",
		"fact.fact_kind = kind.fact_kind",
		"fact.is_tombstone = FALSE",
		"OFFSET 0",
		"generation.status = 'active'",
	} {
		if !strings.Contains(sourceOnlySQL, want) {
			t.Fatalf("source-only support SQL missing %q:\n%s", want, sourceOnlySQL)
		}
	}
	if strings.Contains(sourceOnlySQL, "fact.fact_kind = ANY(") {
		t.Fatalf("source-only support SQL must probe one fact kind per LATERAL row, not filter fact_kind = ANY:\n%s", sourceOnlySQL)
	}

	// The row read probes one literal kind per active scope/generation, so it
	// has no kind cross join to fan out over (#7138).
	targetSQL, _ := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		TargetKind: "repository", TargetID: "repo-x", Limit: 10,
	})
	for _, want := range []string{
		"CROSS JOIN LATERAL (",
		"fact.scope_id = scope.scope_id",
		"fact.generation_id = scope.active_generation_id",
		"fact.fact_kind = 'work_item.external_link'",
		"fact.is_tombstone = FALSE",
		"OFFSET 0",
		"generation.status = 'active'",
	} {
		if !strings.Contains(targetSQL, want) {
			t.Fatalf("target support SQL missing %q:\n%s", want, targetSQL)
		}
	}
	if strings.Contains(targetSQL, "unnest(") || strings.Contains(targetSQL, "fact.fact_kind = ANY(") {
		t.Fatalf("target support SQL probes a single literal kind and must not fan out over a kind array:\n%s", targetSQL)
	}
}

// TestServiceStoryTargetSupportSQLInlinesKindLiteralsForIndex guards #7126: the
// source-only statement carries the support kinds as a literal IN list inside
// the LATERAL, beside the bound array and the kind cross join, so the planner
// can prove migration 123's partial index predicate. Removing the literals
// silently falls back to the wide scope/generation index. The row read's index
// (migration 151) is bound by TestServiceStoryTargetSupportLinkIndexMatchesQuery.
func TestServiceStoryTargetSupportSQLInlinesKindLiteralsForIndex(t *testing.T) {
	t.Parallel()

	sourceOnlySQL, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	want := "fact.fact_kind IN (" + serviceStoryTargetSupportKindLiterals() + ")"
	if !strings.Contains(sourceOnlySQL, want) {
		t.Fatalf("source-only support SQL missing literal kind list %q:\n%s", want, sourceOnlySQL)
	}
	_, lateral, found := strings.Cut(sourceOnlySQL, "CROSS JOIN LATERAL (")
	if !found {
		t.Fatalf("source-only support SQL lost its LATERAL probe:\n%s", sourceOnlySQL)
	}
	if strings.Index(lateral, want) > strings.Index(lateral, "OFFSET 0") {
		t.Fatalf("source-only support SQL must carry the literal kind list inside the LATERAL, before OFFSET 0:\n%s", sourceOnlySQL)
	}
	if !strings.Contains(sourceOnlySQL, serviceStoryTargetSupportUnlinkedPredicate) {
		t.Fatalf("source-only support SQL missing the two-valued unlinked predicate:\n%s", sourceOnlySQL)
	}
	if strings.Contains(sourceOnlySQL, "jsonb_array_length") {
		t.Fatalf("source-only support SQL still uses the three-valued jsonb_array_length predicate:\n%s", sourceOnlySQL)
	}
}

// TestServiceStoryTargetSupportIndexMatchesQuery binds the builder's kind list
// to migration 123, derived from the builder rather than hand-copied, so a kind
// added to the Go list without a new migration fails here instead of silently
// disabling the index for that kind's probes.
func TestServiceStoryTargetSupportIndexMatchesQuery(t *testing.T) {
	t.Parallel()

	migration := normalizeSQLWhitespace(migrationSQLByName(t, "fact_records_story_support_kinds_idx"))
	want := normalizeSQLWhitespace("fact_kind IN (" + serviceStoryTargetSupportKindLiterals() + ")")
	if !strings.Contains(migration, want) {
		t.Fatalf("migration 123 does not carry the query's kind list %q:\n%s", want, migration)
	}
	if !strings.Contains(migration, "is_tombstone = FALSE") {
		t.Fatalf("migration 123 lost the tombstone predicate the statements carry:\n%s", migration)
	}
}
