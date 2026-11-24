// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
	"time"
)

// Admission-order RED proof for #11075 (audit row 11 of the re-run contract,
// "R freeze" in its addendum).
//
// The defect: admitWinnersKeyedWindowSQL admits the oldest ChunkMaxDirtyRows
// rows by seq, while the watermark horizon is min(captured_at) of the
// remainder. The scope-remainder convert (convertWinnersKeyedScopeRemainderSQL)
// writes its debt at a NEW tail seq that keeps the OLD captured_at, and its
// conflict arm keeps an existing queued row's seq while LEAST lowers its
// captured_at. Either way the oldest capture can sit behind many windows of
// newer rows. Each tick then consumes newer rows, the oldest row stays, and
// the watermark cannot pass it until FIFO by seq reaches it (on the QA
// environment, about 34k rows behind the head with the watermark frozen at
// 18:04:45). The fixed admission orders by (captured_at, seq), so the oldest
// debt is admitted first.
//
// This test asserts the FIXED behavior and FAILS on current code (RED stage:
// no production edit).

// admissionOrderRow is one queued dirty-log row's admission key.
type admissionOrderRow struct {
	seq        int64
	capturedAt time.Time
}

// before reports whether a sorts before b in (captured_at, seq) order.
func (a admissionOrderRow) before(b admissionOrderRow) bool {
	if !a.capturedAt.Equal(b.capturedAt) {
		return a.capturedAt.Before(b.capturedAt)
	}
	return a.seq < b.seq
}

// admissionOrderSnapshot reads every queued dirty-log row by seq.
func admissionOrderSnapshot(h *keyedHarness) map[int64]admissionOrderRow {
	h.t.Helper()
	rows, err := h.db.QueryContext(h.ctx, `SELECT seq, captured_at FROM supply_chain_impact_winners_dirty_log`)
	if err != nil {
		h.t.Fatalf("read dirty log: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]admissionOrderRow{}
	for rows.Next() {
		var r admissionOrderRow
		if err := rows.Scan(&r.seq, &r.capturedAt); err != nil {
			h.t.Fatalf("scan dirty log row: %v", err)
		}
		r.capturedAt = r.capturedAt.UTC()
		out[r.seq] = r
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("read dirty log: %v", err)
	}
	return out
}

// admissionOrderMoveStaged moves the staged marks that match a predicate into
// the dirty log with the given captured_at expression. Rows inserted by a
// later statement take later seqs.
const admissionOrderMoveStaged = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
SELECT kind, source, package_id, repository_id, subject_digest, c0, advisory_id, %s
FROM supply_chain_impact_winners_dirty_staging
WHERE %s
ORDER BY seq`

// TestSupplyChainImpactWinnersResweepKeyedAdmitsTheOldestCapturedAtFirstLive
// seeds a dirty log whose oldest capture sits on the tail seq behind three
// admission windows of newer rows, the shape the scope-remainder convert
// leaves, and requires the first bounded tick to admit and apply that row so
// the watermark advances. While draining it also pins the invariants the
// reorder must keep: every admitted row applies exactly once (no row is
// re-admitted), each tick consumes only rows no newer than every row it
// leaves, the watermark never regresses and never covers a queued row, and the
// drained table equals a fresh full recompute.
func TestSupplyChainImpactWinnersResweepKeyedAdmitsTheOldestCapturedAtFirstLive(t *testing.T) {
	h := newKeyedHarness(t, "#11075 winners admission order proof")
	scope := "vuln-intel://11075/admission-order"
	generation := scope + "/g1"
	h.seedScope(scope, generation)

	// Six identifiers that will hold newer captures and one that will hold
	// the oldest. Each identifier is one dirty fact row.
	h.write(scope, generation, append(chunkedFindings("aaa", 6, 1, 0), chunkedFindings("zzz", 1, 1, 0)...)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	// Rewrite every finding so all seven identifiers stage a mark, then move
	// the marks into the log in two statements: the newer rows first, then
	// the oldest capture at the tail seq, as the scope-remainder convert
	// writes its debt.
	h.write(scope, generation, append(chunkedFindings("aaa", 6, 1, 1000), chunkedFindings("zzz", 1, 1, 1000)...)...)
	if depth := h.pendingDepth(); depth != 7 {
		t.Fatalf("rewrite queued %d pending rows, want the 7 staged fact rows", depth)
	}
	// The oldest capture lands halfway between the full resweep's watermark
	// and now, so it is older than every newer row yet still above the
	// watermark: the debt is real and uncovered, as on the QA environment.
	h.exec(fmt.Sprintf(admissionOrderMoveStaged, "now()", "package_id NOT LIKE 'pkg:npm/zzz%'"))
	h.exec(fmt.Sprintf(admissionOrderMoveStaged,
		`(SELECT m.materialized_at + (now() - m.materialized_at) / 2
          FROM supply_chain_impact_winners_materialization AS m WHERE m.singleton = 1)`,
		"package_id LIKE 'pkg:npm/zzz%'"))
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)

	const window = 2
	var oldestSeq, maxSeq int64
	if err := h.db.QueryRowContext(h.ctx, `
SELECT (SELECT seq FROM supply_chain_impact_winners_dirty_log ORDER BY captured_at, seq LIMIT 1),
       (SELECT max(seq) FROM supply_chain_impact_winners_dirty_log)`).Scan(&oldestSeq, &maxSeq); err != nil {
		t.Fatalf("read oldest dirty row: %v", err)
	}
	if oldestSeq != maxSeq {
		t.Fatalf("oldest capture sits at seq %d, want the tail seq %d", oldestSeq, maxSeq)
	}
	if ahead := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE seq < $1`, oldestSeq); ahead <= window {
		t.Fatalf("%d newer rows ahead of the oldest capture, want more than one %d-row window", ahead, window)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE seq = $1 AND package_id LIKE 'pkg:npm/zzz%'`, oldestSeq); n != 1 {
		t.Fatalf("the oldest capture is not the zzz row (matched %d)", n)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Fatalf("%d queued rows are already under the watermark before the first tick", n)
	}

	h.store.ChunkMaxDirtyRows = window
	// The identifier bound stays above the window, so every admitted row
	// applies in its own tick and a row left behind is a re-admission.
	h.store.ChunkMaxIdentifiers = 200

	initial := admissionOrderSnapshot(h)
	queued := initial
	before, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty before the first tick")
	}
	consumed := map[int64]int{}
	previousMark := chunkedWatermark(h)
	for tick := 1; len(queued) > 0; tick++ {
		if tick > 10 {
			t.Fatalf("dirty log never drained after 10 ticks (depth %d)", len(queued))
		}
		result := chunkedTick(h)
		after := admissionOrderSnapshot(h)

		var removed []admissionOrderRow
		for seq, row := range queued {
			if _, still := after[seq]; !still {
				removed = append(removed, row)
				consumed[seq]++
			}
		}
		for seq := range after {
			if _, was := queued[seq]; !was {
				t.Fatalf("tick %d added dirty row seq %d to a log no writer touched", tick, seq)
			}
		}
		if want := min(window, len(queued)); len(removed) != want {
			t.Errorf("tick %d consumed %d rows, want %d: an admitted row was left for re-admission (result %+v)", tick, len(removed), want, result)
		}
		for _, gone := range removed {
			for _, left := range after {
				if left.before(gone) {
					t.Errorf("tick %d consumed seq %d (captured %v) while older seq %d (captured %v) stayed queued",
						tick, gone.seq, gone.capturedAt, left.seq, left.capturedAt)
				}
			}
		}

		mark := chunkedWatermark(h)
		if mark.Before(previousMark) {
			t.Errorf("tick %d watermark %v regressed from %v", tick, mark, previousMark)
		}
		if n := horizonUncovered(h, mark); n != 0 {
			t.Errorf("tick %d: %d queued rows have captured_at at or under the watermark %v", tick, n, mark)
		}
		if tick == 1 {
			if _, still := after[oldestSeq]; still {
				t.Errorf("the oldest capture (seq %d) is still queued after the first tick: admission took the %d oldest rows by seq, not by captured_at", oldestSeq, window)
			}
			if next, ok := horizonRemainingMin(h); ok {
				if !next.After(before) {
					t.Errorf("min(captured_at) after the first tick = %v, want it advanced past %v", next, before)
				}
				if !mark.Equal(next.Add(-time.Microsecond)) {
					t.Errorf("watermark after the first tick = %v, want min(captured_at) %v minus 1us", mark, next)
				}
			}
			if !mark.After(before) {
				t.Errorf("watermark after the first tick = %v, want it past the oldest capture %v", mark, before)
			}
		}
		previousMark = mark
		queued = after
	}

	for seq := range initial {
		if consumed[seq] != 1 {
			t.Errorf("dirty row seq %d was consumed %d times, want exactly once", seq, consumed[seq])
		}
	}
	if depth := h.pendingDepth(); depth != 0 {
		t.Errorf("pending depth after the drain = %d, want 0", depth)
	}
	h.assertParity("admission order drain")
}
