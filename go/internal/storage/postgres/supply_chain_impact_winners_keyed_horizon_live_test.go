// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Horizon proofs for #10706 (winners keyed resweep: watermark horizon +
// drain capacity). A bounded tick stamps the horizon H = min(captured_at)
// over the remaining dirty log minus 1us, so the watermark advances on every
// tick while never stamping past an unapplied capture. A drained tick stamps
// the DB-clock tick stamp S. Both are DB-clock sourced.

func horizonRemainingMin(h *keyedHarness) (time.Time, bool) {
	h.t.Helper()
	var min sql.NullTime
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT min(captured_at) FROM (SELECT captured_at FROM supply_chain_impact_winners_dirty_log UNION ALL SELECT captured_at FROM supply_chain_impact_winners_dirty_staging) AS pending`).Scan(&min); err != nil {
		h.t.Fatalf("read remaining min captured_at: %v", err)
	}
	if !min.Valid {
		return time.Time{}, false
	}
	return min.Time.UTC(), true
}

func horizonUncovered(h *keyedHarness, watermark time.Time) int {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM (SELECT captured_at FROM supply_chain_impact_winners_dirty_log UNION ALL SELECT captured_at FROM supply_chain_impact_winners_dirty_staging) AS pending WHERE captured_at <= $1`, watermark)
}

func horizonDBNow(h *keyedHarness) time.Time {
	h.t.Helper()
	var now time.Time
	if err := h.db.QueryRowContext(h.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		h.t.Fatalf("read DB clock: %v", err)
	}
	return now.UTC()
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonAdvancesOnBoundedTickLive
// proves the #10706 horizon: a bounded tick stamps H = min(captured_at) of
// the remainder minus 1us, advances the watermark, and leaves no remaining
// row at or under the watermark.
func TestSupplyChainImpactWinnersResweepKeyedHorizonAdvancesOnBoundedTickLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 horizon proof")
	h.seedScope("vuln-intel://10706/horizon", "vuln-intel://10706/horizon/g1")

	h.write("vuln-intel://10706/horizon", "vuln-intel://10706/horizon/g1", chunkedFindings("horizon", 12, 2, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.write("vuln-intel://10706/horizon", "vuln-intel://10706/horizon/g1", chunkedFindings("horizon", 12, 2, 1000)...)

	h.store.ChunkMaxIdentifiers = 10
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("first tick over 24 dirty identifiers with a 10-identifier bound = %+v, want Bounded", first)
	}
	if got := chunkedWatermark(h); !got.After(before) {
		t.Fatalf("bounded tick left the watermark at %v, want it advanced past %v", got, before)
	}
	if !first.Watermark.Equal(chunkedWatermark(h)) {
		t.Errorf("bounded tick result watermark = %v, want the committed watermark %v (#10706 F4)", first.Watermark, chunkedWatermark(h))
	}
	wantMin, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty after a bounded tick: the bound did not bound the transaction")
	}
	if got := chunkedWatermark(h); !got.Equal(wantMin.Add(-time.Microsecond)) {
		t.Errorf("watermark after a bounded tick = %v, want min(captured_at) %v minus 1us", got, wantMin)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark: the stamp covers unapplied captures", n)
	}
	chunkedDrain(h)
	h.assertParity("horizon drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonNeverRegressesAcrossTicksLive
// proves watermark monotonicity across ticks with new arrivals: every tick
// advances past the pre-backlog watermark and no tick moves it backward.
func TestSupplyChainImpactWinnersResweepKeyedHorizonNeverRegressesAcrossTicksLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 horizon monotonicity proof")
	h.seedScope("vuln-intel://10706/monotonic", "vuln-intel://10706/monotonic/g1")

	h.write("vuln-intel://10706/monotonic", "vuln-intel://10706/monotonic/g1", chunkedFindings("mono", 8, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.write("vuln-intel://10706/monotonic", "vuln-intel://10706/monotonic/g1", chunkedFindings("mono", 8, 1, 1000)...)
	h.store.ChunkMaxIdentifiers = 3
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("first tick = %+v, want Bounded", first)
	}
	one := chunkedWatermark(h)
	if !one.After(before) {
		t.Fatalf("first bounded tick left the watermark at %v, want it advanced past %v", one, before)
	}
	if n := horizonUncovered(h, one); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the first watermark", n)
	}

	h.write("vuln-intel://10706/monotonic", "vuln-intel://10706/monotonic/g1", chunkedFindings("mono", 8, 1, 2000)...)
	second := chunkedTick(h)
	if !second.Bounded {
		t.Fatalf("second tick = %+v, want Bounded (5 rows still queued plus 8 new)", second)
	}
	two := chunkedWatermark(h)
	if two.Before(one) {
		t.Errorf("second watermark %v regressed past the first %v", two, one)
	}
	if n := horizonUncovered(h, two); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the second watermark", n)
	}
	chunkedDrain(h)
	h.assertParity("monotonic drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonScopeRemainderLive proves the
// horizon covers the scope-remainder conversion: a partially applied scope
// leaves fact rows queued with fresh conversion-time captures, and the
// watermark stays under every one of them.
func TestSupplyChainImpactWinnersResweepKeyedHorizonScopeRemainderLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 scope-remainder horizon proof")
	scope := "vuln-intel://10706/scope"
	h.seedScope(scope, scope+"/g1")
	h.write(scope, scope+"/g1", chunkedFindings("act", 8, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.addGeneration(scope, scope+"/g2", "pending")
	h.write(scope, scope+"/g2", chunkedFindings("act", 8, 1, 1000)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'fact'`)
	h.activate(scope, scope+"/g1", scope+"/g2")

	h.store.ChunkMaxIdentifiers = 3
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("scope-split tick = %+v, want Bounded", first)
	}
	if got := chunkedWatermark(h); !got.After(before) {
		t.Fatalf("bounded scope tick left the watermark at %v, want it advanced past %v", got, before)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`); n == 0 {
		t.Error("no converted remainder fact rows queued: the unapplied scope expansion was lost with the scope row")
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark", n)
	}
	chunkedDrain(h)
	h.assertParity("scope-remainder horizon drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonLateCommitCoveredNextTickLive
// proves the uncommitted-capture bound: a row whose INSERT executed before
// the tick but committed after it stays queued, is never covered by the
// tick's stamp, and is applied on the very next tick.
func TestSupplyChainImpactWinnersResweepKeyedHorizonLateCommitCoveredNextTickLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 late-commit horizon proof")
	h.seedScope("vuln-intel://10706/late", "vuln-intel://10706/late/g1")

	h.write("vuln-intel://10706/late", "vuln-intel://10706/late/g1", chunkedFindings("late", 12, 2, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.write("vuln-intel://10706/late", "vuln-intel://10706/late/g1", chunkedFindings("late", 12, 2, 1000)...)
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin late writer: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:npm/late-00', 'repo-9805', '', 'CVE-2026-late-00', '')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert uncommitted late row: %v", err)
	}
	h.store.ChunkMaxIdentifiers = 10
	first := chunkedTick(h)
	if !first.Bounded {
		_ = tx.Rollback()
		t.Fatalf("first tick = %+v, want Bounded", first)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit late writer: %v", err)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-late-00'`); n != 1 {
		t.Fatalf("%d late rows queued after the tick it was invisible to, want exactly the committed one", n)
	}
	wm := chunkedWatermark(h)
	if !wm.After(before) {
		t.Fatalf("bounded tick left the watermark at %v, want it advanced past %v", wm, before)
	}
	var lateAt time.Time
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT captured_at FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-late-00'`).Scan(&lateAt); err != nil {
		t.Fatalf("read late row capture time: %v", err)
	}
	if !lateAt.UTC().After(wm) {
		t.Errorf("late row captured at %v is covered by watermark %v: the tick stamped past an unapplied capture", lateAt.UTC(), wm)
	}
	if n := horizonUncovered(h, wm); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark", n)
	}
	h.store.ChunkMaxIdentifiers = 1000
	second := chunkedTick(h)
	if second.Bounded {
		t.Errorf("tick after the late commit = %+v, want the late row applied and the log drained", second)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-late-00'`); n != 0 {
		t.Errorf("%d late rows still queued after the next tick: exposure exceeded one tick", n)
	}
	h.assertParity("late-commit drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonTupleOrderIrrelevantLive
// proves the horizon is a property of the residual set, not of admission
// order or tuple order: the chunk applies the oldest identifiers first even
// when they sort last alphabetically, and the watermark is still the min
// over the newer rows left behind.
func TestSupplyChainImpactWinnersResweepKeyedHorizonTupleOrderIrrelevantLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 tuple-order horizon proof")
	h.seedScope("vuln-intel://10706/order", "vuln-intel://10706/order/g1")

	h.write("vuln-intel://10706/order", "vuln-intel://10706/order/g1", chunkedFindings("zzz", 4, 1, 0)...)
	h.write("vuln-intel://10706/order", "vuln-intel://10706/order/g1", chunkedFindings("aaa", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	h.write("vuln-intel://10706/order", "vuln-intel://10706/order/g1", chunkedFindings("zzz", 4, 1, 1000)...)
	h.write("vuln-intel://10706/order", "vuln-intel://10706/order/g1", chunkedFindings("aaa", 4, 1, 1000)...)
	if depth := h.pendingDepth(); depth == 0 {
		t.Fatal("rewrite queued no dirty rows")
	}
	// Age decouples the chunk order from tuple order: the last-sorting zzz
	// rows carry the oldest captures, so the oldest-first chunk applies them
	// even though the aaa identifiers sort before them.
	h.exec(`UPDATE supply_chain_impact_winners_dirty_log SET captured_at = now() - interval '1 hour' WHERE package_id LIKE 'pkg:npm/zzz%'`)
	h.exec(`UPDATE supply_chain_impact_winners_dirty_log SET captured_at = now() WHERE package_id LIKE 'pkg:npm/aaa%'`)

	h.store.ChunkMaxIdentifiers = 4
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("order tick = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers != 4 {
		t.Errorf("order tick probed %d identifiers, want the 4-identifier chunk", first.ChunkIdentifiers)
	}
	// The oldest identifiers apply first despite sorting last: every zzz row
	// is gone and the horizon is the newer aaa remainder's min.
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE package_id LIKE 'pkg:npm/zzz%'`); n != 0 {
		t.Errorf("%d zzz rows still queued, want the oldest chunk applied first", n)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE package_id LIKE 'pkg:npm/aaa%'`); n == 0 {
		t.Error("no aaa rows queued: the newer rows the oldest-first chunk skipped are gone")
	}
	wantMin, ok := horizonRemainingMin(h)
	if !ok {
		t.Fatal("dirty log is empty after a bounded tick")
	}
	if got := chunkedWatermark(h); !got.Equal(wantMin.Add(-time.Microsecond)) {
		t.Errorf("watermark after the order tick = %v, want min(captured_at) %v minus 1us", got, wantMin)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark", n)
	}
	chunkedDrain(h)
	h.assertParity("tuple-order drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonDbSourcedStampLive proves the
// tick stamp lives in the DB clock domain: after both a bounded and a
// drained tick the watermark tracks clock_timestamp(), not the Go-supplied
// stamp (the harness clock is days behind the DB clock).
func TestSupplyChainImpactWinnersResweepKeyedHorizonDbSourcedStampLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 DB-sourced stamp proof")
	h.seedScope("vuln-intel://10706/clock", "vuln-intel://10706/clock/g1")

	h.write("vuln-intel://10706/clock", "vuln-intel://10706/clock/g1", chunkedFindings("clock", 8, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	h.write("vuln-intel://10706/clock", "vuln-intel://10706/clock/g1", chunkedFindings("clock", 8, 1, 1000)...)
	h.store.ChunkMaxIdentifiers = 3
	if first := chunkedTick(h); !first.Bounded {
		t.Fatalf("first tick = %+v, want Bounded", first)
	}
	assertNearDBNow := func(label string) {
		t.Helper()
		wm, dbNow := chunkedWatermark(h), horizonDBNow(h)
		if dt := dbNow.Sub(wm); dt < 0 || dt > 5*time.Minute {
			t.Errorf("watermark %v after the %s is %v from the DB clock %v: the stamp is not DB-sourced", wm, label, dt, dbNow)
		}
	}
	assertNearDBNow("bounded tick")
	chunkedDrain(h)
	assertNearDBNow("drained tick")
	h.assertParity("DB-sourced stamp drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonIdleLateCommitLive proves the
// idle path never covers a capture that commits after the tick's stamp: a row
// invisible to the tick (its INSERT is uncommitted at admission and at the
// residual count) but captured past the pre-admission stamp stays queued
// through the tick, the result reports the committed stamp, and the next tick
// applies it.
func TestSupplyChainImpactWinnersResweepKeyedHorizonIdleLateCommitLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 idle late-commit proof")
	h.seedScope("vuln-intel://10706/idlelate", "vuln-intel://10706/idlelate/g1")

	h.write("vuln-intel://10706/idlelate", "vuln-intel://10706/idlelate/g1", chunkedFindings("idle", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	// The late row's capture time is past any stamp this tick can write, the
	// way a capture that executes mid-tick carries a later captured_at than
	// the pre-admission stamp. It stays uncommitted, so the tick neither
	// admits nor counts it.
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin late writer: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', 'pkg:npm/idle-late-00', 'repo-9805', '', 'CVE-2026-idle-late-00', '',
    clock_timestamp() + interval '1 hour')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert uncommitted late row: %v", err)
	}
	idle := chunkedTick(h)
	if idle.Bounded || idle.Remaining != 0 {
		_ = tx.Rollback()
		t.Fatalf("idle tick = %+v, want a non-bounded tick with nothing remaining", idle)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit late writer: %v", err)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-idle-late-00'`); n != 1 {
		t.Fatalf("%d late rows queued after the tick it was invisible to, want exactly the committed one", n)
	}
	if wm := chunkedWatermark(h); !idle.Watermark.Equal(wm) {
		t.Errorf("idle tick result watermark = %v, want the committed watermark %v", idle.Watermark, wm)
	} else if n := horizonUncovered(h, wm); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark: the tick stamped past a capture that committed after its stamp", n)
	}
	second := chunkedTick(h)
	if second.Bounded {
		t.Errorf("tick after the late commit = %+v, want the late row applied and the log drained", second)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-idle-late-00'`); n != 0 {
		t.Errorf("%d late rows still queued after the next tick", n)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark after the repair tick", n)
	}
	h.assertParity("idle late-commit drain")
}

// TestSupplyChainImpactWinnersResweepKeyedHorizonDrainedLateCommitLive proves
// the drained-apply path repairs the one-tick over-claim the design accepts:
// a row whose INSERT executed before the tick's stamp but committed after it
// is covered by that stamp while still queued, the result reports the
// committed stamp, and the very next tick applies the row.
func TestSupplyChainImpactWinnersResweepKeyedHorizonDrainedLateCommitLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 drained late-commit proof")
	h.seedScope("vuln-intel://10706/drainlate", "vuln-intel://10706/drainlate/g1")

	h.write("vuln-intel://10706/drainlate", "vuln-intel://10706/drainlate/g1", chunkedFindings("drain", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	h.write("vuln-intel://10706/drainlate", "vuln-intel://10706/drainlate/g1", chunkedFindings("drain", 4, 1, 1000)...)
	// The late row's INSERT executes before the tick reads its stamp, but it
	// commits after the tick, so the tick stamps past it without applying it.
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin late writer: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:npm/drain-late-00', 'repo-9805', '', 'CVE-2026-drain-late-00', '')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert uncommitted late row: %v", err)
	}
	h.store.ChunkMaxIdentifiers = 1000
	first := chunkedTick(h)
	if first.Bounded {
		_ = tx.Rollback()
		t.Fatalf("drained tick = %+v, want the visible rows applied with nothing left", first)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit late writer: %v", err)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-drain-late-00'`); n != 1 {
		t.Fatalf("%d late rows queued after the tick it was invisible to, want exactly the committed one", n)
	}
	wm := chunkedWatermark(h)
	if !first.Watermark.Equal(wm) {
		t.Errorf("drained tick result watermark = %v, want the committed watermark %v", first.Watermark, wm)
	}
	// The late row executed before the stamp but committed after it: the one
	// accepted tick of over-claim. The next tick repairs it.
	if n := horizonUncovered(h, wm); n != 1 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark, want the one late row the stamp covered", n)
	}
	second := chunkedTick(h)
	if second.Bounded {
		t.Errorf("tick after the late commit = %+v, want the late row applied and the log drained", second)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE c0 = 'CVE-2026-drain-late-00'`); n != 0 {
		t.Errorf("%d late rows still queued after the next tick: exposure exceeded one tick", n)
	}
	if n := horizonUncovered(h, chunkedWatermark(h)); n != 0 {
		t.Errorf("%d remaining rows have captured_at at or under the watermark after the repair tick", n)
	}
	h.assertParity("drained late-commit drain")
}

// TestSupplyChainImpactWinnersResweepKeyedFullFallbackReportsCommittedWatermarkLive
// proves the over-cap fallback reports the re-read committed watermark on its
// result, the same contract every other branch keeps (#10706 F4).
func TestSupplyChainImpactWinnersResweepKeyedFullFallbackReportsCommittedWatermarkLive(t *testing.T) {
	h := newKeyedHarness(t, "#10706 fallback committed-watermark proof")
	h.seedScope("vuln-intel://10706/fallback", "vuln-intel://10706/fallback/g1")

	h.write("vuln-intel://10706/fallback", "vuln-intel://10706/fallback/g1", chunkedFindings("fb", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}

	h.write("vuln-intel://10706/fallback", "vuln-intel://10706/fallback/g1", chunkedFindings("fb", 4, 1, 1000)...)
	h.store.MaxIdentifiers = 1
	got := chunkedTick(h)
	if got.Path != telemetry.SupplyChainImpactWinnersPathFull {
		t.Fatalf("over-cap tick ran on path %q, want %q (result %+v)", got.Path, telemetry.SupplyChainImpactWinnersPathFull, got)
	}
	if !got.Watermark.Equal(chunkedWatermark(h)) {
		t.Errorf("fallback result watermark = %v, want the committed watermark %v", got.Watermark, chunkedWatermark(h))
	}
	h.assertParity("fallback committed watermark drain")
}
