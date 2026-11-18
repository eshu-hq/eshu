// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
	"time"
)

// Chunk oldest-first RED proofs for #10706 (arbiter ruling
// docs/internal/arbiter/10706-gatea-ruling-2026-11-15.md).
//
// The defect: selectWinnersKeyedChunkIdentsSQL takes the alphabetical prefix
// of the admitted window's post-expansion identifier set, so when the oldest
// dirty row's identifier sorts last and more earlier-sorting identifiers fill
// the admitted window past the chunk bound, every tick applies the earlier
// prefix, deletes it, and leaves min(captured_at) and the oldest row in place
// while the depth falls. The fixed chunk carries each identifier's age
// (MIN(captured_at), tie-break MIN(seq)) and applies the oldest first.
//
// These tests assert the FIXED behavior and FAIL on current code (RED stage:
// no production edit). They pin the first bounded tick only and do not drain
// to parity. The tests that pin the current alphabetical skip
// (TestSupplyChainImpactWinnersResweepKeyedHorizonTupleOrderIrrelevantLive and
// the zero-applied-scope half of
// TestSupplyChainImpactWinnersResweepKeyedChunkedZeroAppliedScopeLive) are
// deliberately left unchanged here and must be rewritten, not relaxed, with
// the fix.

// TestSupplyChainImpactWinnersResweepKeyedChunkOldestFirstFactLive proves the
// fact arm: the oldest fact row's identifier sorts last alphabetically, four
// earlier-sorting identifiers fill the admitted window past the 2-identifier
// chunk bound, and the first bounded tick must apply the oldest identifier so
// min(captured_at) advances. Current code applies the alphabetical prefix and
// leaves the oldest row and the min in place while the depth falls.
func TestSupplyChainImpactWinnersResweepKeyedChunkOldestFirstFactLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 chunk oldest-first fact proof")
	scope := "vuln-intel://10706/oldest-fact"
	h.seedScope(scope, scope+"/g1")

	// One identifier that sorts last, four that sort before it.
	h.write(scope, scope+"/g1", append(chunkedFindings("zzz", 1, 1, 0), chunkedFindings("aaa", 4, 1, 0)...)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	// Rewrite every finding so all five identifiers go dirty in one window,
	// then age the last-sorting row into the oldest capture: the oldest fact
	// row's identifier sorts last alphabetically.
	h.write(scope, scope+"/g1", append(chunkedFindings("zzz", 1, 1, 1000), chunkedFindings("aaa", 4, 1, 1000)...)...)
	if depth := h.pendingDepth(); depth != 5 {
		t.Fatalf("rewrite queued %d pending rows, want the 5 staged fact rows", depth)
	}
	h.exec(`UPDATE supply_chain_impact_winners_dirty_staging SET captured_at = now() - interval '1 hour' WHERE package_id LIKE 'pkg:npm/zzz%'`)
	h.exec(`UPDATE supply_chain_impact_winners_dirty_staging SET captured_at = now() WHERE package_id NOT LIKE 'pkg:npm/zzz%'`)
	before, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty before the bounded tick")
	}
	var oldestPkg string
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT package_id FROM supply_chain_impact_winners_dirty_staging ORDER BY captured_at, seq LIMIT 1`).Scan(&oldestPkg); err != nil {
		t.Fatalf("read oldest staged row: %v", err)
	}
	if oldestPkg != "pkg:npm/zzz-00" {
		t.Fatalf("oldest dirty row is %q, want the last-sorting pkg:npm/zzz-00", oldestPkg)
	}

	// Four earlier-sorting identifiers in the admitted window, past the
	// 2-identifier chunk bound.
	h.store.ChunkMaxIdentifiers = 2
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("oldest-first tick = %+v, want Bounded (5 identifiers past a 2-identifier bound)", first)
	}
	if depth := h.dirtyDepth(); depth != 3 {
		t.Errorf("dirty log depth after the bounded tick = %d, want 3 (one 2-identifier chunk consumed)", depth)
	}
	// Fixed behavior: the oldest identifier applies in the first chunk, so
	// the oldest row leaves and the min advances to the next remainder.
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE package_id LIKE 'pkg:npm/zzz%'`); n != 0 {
		t.Errorf("%d zzz rows still queued: the oldest identifier did not apply in the first chunk", n)
	}
	after, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty after a bounded tick")
	}
	if !after.After(before) {
		t.Errorf("min(captured_at) after the bounded tick = %v, want it advanced past %v", after, before)
	}
	if got := chunkedWatermark(h); !got.Equal(after.Add(-time.Microsecond)) {
		t.Errorf("watermark after the bounded tick = %v, want min(captured_at) %v minus 1us", got, after)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark", n)
	}
}

// TestSupplyChainImpactWinnersResweepKeyedChunkOldestFirstScopeLive proves the
// scope-provenance arm: the oldest scope row's expansion sorts last
// alphabetically, four earlier-sorting fact identifiers fill the admitted
// window past the 2-identifier chunk bound, and the first bounded tick must
// apply the scope row's identifier (by the scope row's age) instead of
// skipping the scope row with zero identifiers applied. Current code applies
// the alphabetical prefix and leaves the oldest scope row and the min in
// place.
func TestSupplyChainImpactWinnersResweepKeyedChunkOldestFirstScopeLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 chunk oldest-first scope proof")
	scope := "vuln-intel://10706/oldest-scope"
	h.seedScope(scope, scope+"/g1")
	h.write(scope, scope+"/g1", chunkedFindings("aaa", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	// The oldest scope row's expansion sorts last: g2 holds the one zzz
	// identifier, g1 the four earlier-sorting aaa identifiers.
	h.addGeneration(scope, scope+"/g2", "pending")
	h.write(scope, scope+"/g2", chunkedFindings("zzz", 1, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'fact'`)
	h.write(scope, scope+"/g1", chunkedFindings("aaa", 4, 1, 1000)...)
	h.activate(scope, scope+"/g1", scope+"/g2")
	if depth := h.pendingDepth(); depth != 6 {
		t.Fatalf("queued %d pending rows, want 4 staged fact rows plus 2 staged scope rows", depth)
	}
	h.exec(`UPDATE supply_chain_impact_winners_dirty_staging SET captured_at = now() - interval '1 hour' WHERE kind = 'scope' AND generation_id = $1`, scope+"/g2")
	h.exec(`UPDATE supply_chain_impact_winners_dirty_staging SET captured_at = now() WHERE NOT (kind = 'scope' AND generation_id = $1)`, scope+"/g2")
	before, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty before the bounded tick")
	}
	var oldestKind, oldestGen string
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT kind, generation_id FROM supply_chain_impact_winners_dirty_staging ORDER BY captured_at, seq LIMIT 1`).Scan(&oldestKind, &oldestGen); err != nil {
		t.Fatalf("read oldest staged row: %v", err)
	}
	if oldestKind != "scope" || oldestGen != scope+"/g2" {
		t.Fatalf("oldest dirty row is %s %q, want the scope row of the last-sorting expansion", oldestKind, oldestGen)
	}

	h.store.ChunkMaxIdentifiers = 2
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("oldest-first scope tick = %+v, want Bounded (5 identifiers past a 2-identifier bound)", first)
	}
	if depth := h.dirtyDepth(); depth == 0 {
		t.Error("bounded scope tick drained the log: the unapplied identifiers were lost with their rows")
	}
	// Fixed behavior: the scope row's age pulls its last-sorting identifier
	// into the first chunk, so the g2 scope row leaves (fully applied) and
	// the min advances to the next remainder.
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND generation_id = $1`, scope+"/g2"); n != 0 {
		t.Errorf("%d g2 scope rows still queued: the oldest scope expansion saw zero applied identifiers", n)
	}
	after, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty after a bounded tick")
	}
	if !after.After(before) {
		t.Errorf("min(captured_at) after the bounded scope tick = %v, want it advanced past %v", after, before)
	}
	if got := chunkedWatermark(h); !got.Equal(after.Add(-time.Microsecond)) {
		t.Errorf("watermark after the bounded scope tick = %v, want min(captured_at) %v minus 1us", got, after)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark", n)
	}
}
