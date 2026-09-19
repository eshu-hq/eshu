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
