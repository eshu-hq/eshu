// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// readinessGateMarkers are the fragments that identify the takeability test
// (the claim's readiness gate and the parked-wait anti-join) in SQL text.
var readinessGateMarkers = []string{
	"reducer_claim_readiness_requirements",
	"readiness_phase",
	"reducer_waiting_items",
}

// signalSections splits one signal query into its aggregate select list and
// its WHERE clause (through the end of the statement for the bounded probe,
// which has no GROUP BY).
func signalSections(t *testing.T, name, query string) (selectList, where string) {
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
	return query[selectStart:fromAt], where
}

// TestSignalReadinessGateSitsInTheAggregateFilters pins the measured #11187
// performance decision: the takeability test lives in the aggregate FILTERs of
// the grouped query and the bounded probe, never in their WHERE. A subplan in
// the WHERE flipped the grouped plan to a spilling sort aggregate (235 ms to
// 610 ms at 10x replay scale) while every behavior test kept passing, so the
// text shape is the only hermetic guard. Four aggregates filter on it: the
// oldest age, the depth, and the two aged-row counts.
func TestSignalReadinessGateSitsInTheAggregateFilters(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"grouped": sourceClaimableSignalQuery,
		"bounded": boundedSourceSignalQuery(),
	} {
		selectList, where := signalSections(t, name, query)
		if got := strings.Count(selectList, claimableReadySQL); got != 4 {
			t.Fatalf("%s: the takeability test appears in %d aggregate FILTERs, want 4 (oldest age, depth, past-engage, past-release)", name, got)
		}
		for _, marker := range readinessGateMarkers {
			if strings.Contains(where, marker) {
				t.Fatalf("%s: %q appears in the WHERE: a subplan there flips the grouped plan to a spilling sort aggregate (235 ms to 610 ms at 10x)", name, marker)
			}
		}
		for _, marker := range readinessGateMarkers[:2] {
			if !strings.Contains(selectList, marker) {
				t.Fatalf("%s: the aggregate FILTERs lost %q", name, marker)
			}
		}
	}
}
