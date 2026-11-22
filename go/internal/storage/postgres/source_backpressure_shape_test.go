// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// readinessGateMarkers are the fragments that identify the takeability test
// (the claim's readiness gate and the parked-wait probe) in SQL text. None may
// appear in a signal query's WHERE.
var readinessGateMarkers = []string{
	"reducer_claim_readiness_requirements",
	"readiness_phase",
	"reducer_waiting_items",
	"parked_wait",
}

// signalSections splits one signal query into its aggregate select list, its
// FROM section (through the join that precedes the WHERE), and its WHERE clause
// (through the end of the statement for the bounded probe, which has no GROUP
// BY).
func signalSections(t *testing.T, name, query string) (selectList, from, where string) {
	t.Helper()
	selectStart := strings.Index(query, "\nSELECT")
	fromAt := strings.Index(query, "\nFROM active_fact_work_items AS work")
	whereAt := strings.Index(query, "\nWHERE work.stage = 'reducer'")
	if selectStart < 0 || fromAt < selectStart || whereAt < fromAt {
		t.Fatalf("%s: could not locate the SELECT, FROM, and WHERE sections", name)
	}
	where = query[whereAt:]
	if group := strings.Index(where, "\nGROUP BY"); group >= 0 {
		where = where[:group]
	}
	return query[selectStart:fromAt], query[fromAt:whereAt], where
}

// TestSignalReadinessGateSitsInTheAggregateFilters pins the measured #11187
// performance decisions, which every behavior test passes through unchanged
// while the cost moves, so the text shape is the only hermetic guard:
//
//   - the takeability test lives in the aggregate FILTERs of the grouped query
//     and the bounded probe, never in their WHERE: a subplan in the WHERE flipped
//     the grouped plan to a spilling sort aggregate (235 ms to 610 ms at 10x
//     replay scale). Four aggregates filter on it: the oldest age, the depth,
//     and the two aged-row counts;
//   - the parked-wait table is read once per scanned row by one lateral probe in
//     the shared FROM, and that probe is guarded by work.status = 'retrying'
//     (R4-3): a parked row is always retrying, so the guard keeps a pending row
//     from paying a wait-table lookup, and the probe's FILTER term appears in
//     exactly the four aggregates. Four correlated copies, a hashed set, or an
//     unguarded probe measured +18% to +157% on the bounded probe.
func TestSignalReadinessGateSitsInTheAggregateFilters(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"grouped": sourceClaimableSignalQuery,
		"bounded": boundedSourceSignalQuery(),
	} {
		selectList, from, where := signalSections(t, name, query)
		if got := strings.Count(selectList, claimableReadySQL); got != 4 {
			t.Fatalf("%s: the takeability test appears in %d aggregate FILTERs, want 4 (oldest age, depth, past-engage, past-release)", name, got)
		}
		for _, marker := range readinessGateMarkers {
			if strings.Contains(where, marker) {
				t.Fatalf("%s: %q appears in the WHERE: a subplan there flips the grouped plan to a spilling sort aggregate (235 ms to 610 ms at 10x)", name, marker)
			}
		}
		if got := strings.Count(selectList, "parked_wait.parked IS NULL"); got != 4 {
			t.Fatalf("%s: parked_wait.parked IS NULL appears in %d aggregate FILTERs, want exactly 4", name, got)
		}
		for _, marker := range readinessGateMarkers[:2] {
			if got := strings.Count(selectList, marker); got < 4 {
				t.Fatalf("%s: %q appears in %d aggregate FILTERs, want it in all 4", name, marker, got)
			}
		}
		if got := strings.Count(query, "reducer_waiting_items"); got != 1 {
			t.Fatalf("%s: reducer_waiting_items appears %d times in the query, want exactly once (one lateral probe)", name, got)
		}
		if got := strings.Count(from, "reducer_waiting_items"); got != 1 {
			t.Fatalf("%s: reducer_waiting_items appears %d times between FROM and WHERE, want the one lateral probe", name, got)
		}
		if !strings.Contains(from, "LEFT JOIN LATERAL") || !strings.Contains(from, "work.status = 'retrying'") {
			t.Fatalf("%s: the wait-table probe must be a lateral guarded by work.status = 'retrying'; FROM section:\n%s", name, from)
		}
	}
}
