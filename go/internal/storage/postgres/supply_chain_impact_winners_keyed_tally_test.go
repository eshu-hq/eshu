// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestWinnersDrainStatementsKeepTheNVDTripTally pins #10767: both maintainer
// DELETE statements must add the NVD-proxy rows they delete to the drain tally
// in the same statement, and the outer SELECT must keep the per-source
// shape the callers scan (source, count). The live proof in
// storage/postgres/vulnerability drains and samples a real database.
func TestWinnersDrainStatementsKeepTheNVDTripTally(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"applied": deleteWinnersKeyedAppliedDirtySQL,
		"all":     deleteWinnersKeyedAllDirtySQL,
	} {
		for _, want := range []string{
			"INSERT INTO supply_chain_impact_winners_dirty_tally",
			winnersNVDProxyRowPredicate,
			winnersNVDScopeRowPredicate,
			"RETURNING d.source, d.kind, d.captured_at, d.advisory_id, d.scope_id",
			"SELECT source, count(*) FROM deleted GROUP BY source",
		} {
			if name == "all" {
				want = strings.ReplaceAll(want, "d.", "")
			}
			if !strings.Contains(statement, want) {
				t.Errorf("%s drain statement does not contain %q", name, want)
			}
		}
	}
}

// TestWinnersConvertSelectDerivesFromShippedConvert pins that the read-only
// SELECT arm the plan proofs EXPLAIN is a byte-suffix of the shipped convert
// INSERT, so the explained join is the join the tick runs, not a hand copy
// that can drift.
func TestWinnersConvertSelectDerivesFromShippedConvert(t *testing.T) {
	t.Parallel()

	if !strings.HasSuffix(convertWinnersKeyedScopeRemainderSQL, convertWinnersKeyedScopeRemainderSelectSQL) {
		t.Error("convert SELECT arm is not a byte-suffix of the shipped convert INSERT: the plan proof would explain a different join")
	}
	if !strings.HasPrefix(strings.TrimSpace(convertWinnersKeyedScopeRemainderSQL), "INSERT INTO supply_chain_impact_winners_dirty_log") {
		t.Error("shipped convert does not start with the dirty-log INSERT prefix")
	}
}

// TestWinnersCaptureStatementsDoNotTouchTheTally pins the hot-path claim of
// #10767: the tally is written at drain time only, so no capture statement
// the dirty log has in this package names it.
func TestWinnersCaptureStatementsDoNotTouchTheTally(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"capture facts":           captureWinnersDirtyFactsSQL,
		"queue audit repairs":     queueWinnersAuditRepairsSQL,
		"convert scope remainder": convertWinnersKeyedScopeRemainderSQL,
	} {
		if strings.Contains(statement, "winners_dirty_tally") {
			t.Errorf("%s statement writes the drain tally; the capture path must stay untouched", name)
		}
	}
}
