// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"
)

// ajPinPlannerState pins the planner state the measurement starts from:
// autovacuum and autoanalyze are off for the Ack-path tables in this disposable
// database and a VACUUM ANALYZE sets the visibility map and statistics
// once, here. Without this, an autoanalyze (or its absence under CI load)
// flips statement plans mid-measurement and moves buffer hits by tens per
// Ack with identical WAL: one CI attempt read +45 blocks against the 40
// bound where local runs read +11.
func ajPinPlannerState(t *testing.T, reader *sql.DB) {
	t.Helper()
	// Iterate the fixture table set itself so the pin cannot drift from
	// the tables Ack actually writes (activation obligation rows included).
	for _, table := range ajFixtureTables {
		ajExec(t, reader, fmt.Sprintf(`ALTER TABLE %s SET (autovacuum_enabled = false)`, table))
	}
	ajExec(t, reader, `VACUUM ANALYZE`)
}

// ajBackendCountersSettled flushes the Ack backend's pending stats and
// polls until two consecutive snapshots agree, so a batch boundary never
// inherits counters the stats system has not yet flushed. The flush is
// asynchronous (pg_stat_force_next_flush only arms it); under CI load the
// unflushed remainder of the previous batch can land inside the next
// batch's window and move a per-Ack mean by tens of blocks. The poll is
// bounded; on timeout the last snapshot stands and the gate still judges.
func ajBackendCountersSettled(t *testing.T, reader *sql.DB, pid int, flush func()) ajBackendIO {
	t.Helper()
	flush()
	prev := ajBackendCounters(t, reader, pid)
	for range 80 {
		time.Sleep(25 * time.Millisecond)
		flush()
		next := ajBackendCounters(t, reader, pid)
		if next == prev {
			return next
		}
		prev = next
	}
	t.Logf("backend counters did not settle within 2s; using last snapshot %+v", prev)
	return prev
}

// ajPairedMedianDelta is the median of the per-round on-minus-off deltas.
// Rounds are paired: off[i] and on[i] ran in the same round under the same
// instance state, so a round whose environment drifted (stats flush lag, a
// co-tenant burst) contaminates one pair, not the verdict. The mean of
// means the gate used before let one contaminated batch of eight move the
// result past the bound.
func ajPairedMedianDelta(off, on []ajCostSample, field func(ajCostSample) float64) (float64, bool) {
	if len(off) == 0 || len(off) != len(on) {
		return 0, false
	}
	deltas := make([]float64, len(on))
	for i := range on {
		deltas[i] = field(on[i]) - field(off[i])
	}
	sort.Float64s(deltas)
	mid := len(deltas) / 2
	if len(deltas)%2 == 1 {
		return deltas[mid], true
	}
	return (deltas[mid-1] + deltas[mid]) / 2, true
}

// TestAJCostViolationsMedianPairing pins the block bound's aggregation
// without a live database: the median paired delta tolerates one
// contaminated round, a uniform regression still trips, and the n=1
// seeded-RED shape keeps the old single-delta strength.
func TestAJCostViolationsMedianPairing(t *testing.T) {
	t.Parallel()
	sample := func(blocks float64) ajCostSample {
		return ajCostSample{client: ajCostStatementsOnAck, backendBlocks: blocks}
	}
	pairs := func(deltas ...float64) (off, on []ajCostSample) {
		for _, d := range deltas {
			off = append(off, sample(100))
			on = append(on, sample(100+d))
		}
		return off, on
	}
	for name, tc := range map[string]struct {
		deltas []float64
		trips  bool
	}{
		"uniform regression trips":  {[]float64{50, 50, 50, 50}, true},
		"planted single pair trips": {[]float64{284}, true},
		"order oscillation passes":  {[]float64{46, -20, 42, -19}, false},
		"one spiked round passes":   {[]float64{11, 11, 11, 200}, false},
		"at the bound passes":       {[]float64{40, 40, 40, 40}, false},
		"just over the bound trips": {[]float64{41, 41, 41, 41}, true},
	} {
		off, on := pairs(tc.deltas...)
		got := ajCostViolations(off, on, false)
		if trips := len(got) > 0; trips != tc.trips {
			t.Errorf("%s: violations=%v, want trips=%v", name, got, tc.trips)
		}
	}
	// Length mismatch falls back to the mean difference, never a silent pass.
	off := []ajCostSample{sample(100)}
	on := []ajCostSample{sample(150), sample(150)}
	if got := ajCostViolations(off, on, false); len(got) == 0 {
		t.Errorf("mismatched lengths: violations empty, want mean fallback to trip")
	}
}
