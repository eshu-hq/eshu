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

	targetSQL, _ := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{Repository: "repo-x", Limit: 10})
	sourceOnlySQL, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for name, query := range map[string]string{"target": targetSQL, "source-only": sourceOnlySQL} {
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
			if !strings.Contains(query, want) {
				t.Fatalf("%s support SQL missing %q:\n%s", name, want, query)
			}
		}
		if strings.Contains(query, "fact.fact_kind = ANY(") {
			t.Fatalf("%s support SQL must probe one fact kind per LATERAL row, not filter fact_kind = ANY:\n%s", name, query)
		}
	}
}

// TestServiceStoryTargetSupportSQLInlinesKindLiteralsForIndex guards #7126:
// both statements carry the support kinds as a literal IN list inside the
// LATERAL, beside the bound array and the kind cross join, so the planner can
// prove migration 123's partial index predicate. Removing the literals silently
// falls back to the wide scope/generation index.
func TestServiceStoryTargetSupportSQLInlinesKindLiteralsForIndex(t *testing.T) {
	t.Parallel()

	targetSQL, _ := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{Repository: "repo-x", Limit: 10})
	sourceOnlySQL, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	want := "fact.fact_kind IN (" + serviceStoryTargetSupportKindLiterals() + ")"
	for name, query := range map[string]string{"target": targetSQL, "source-only": sourceOnlySQL} {
		if !strings.Contains(query, want) {
			t.Fatalf("%s support SQL missing literal kind list %q:\n%s", name, want, query)
		}
		_, lateral, found := strings.Cut(query, "CROSS JOIN LATERAL (")
		if !found {
			t.Fatalf("%s support SQL lost its LATERAL probe:\n%s", name, query)
		}
		if strings.Index(lateral, want) > strings.Index(lateral, "OFFSET 0") {
			t.Fatalf("%s support SQL must carry the literal kind list inside the LATERAL, before OFFSET 0:\n%s", name, query)
		}
	}
	if !strings.Contains(sourceOnlySQL, documentationNoStructuredRefsPredicate("fact.payload")) {
		t.Fatalf("source-only support SQL missing the two-valued no-refs predicate:\n%s", sourceOnlySQL)
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
