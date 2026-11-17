// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
	"time"
)

// Lane A of eshu-hq/eshu#10827 (arbiter verdict R2: Global Key-Ordered
// Versioned Upsert). The dirty log collapses repeated marks of one recompute
// unit at write time: every multi-row writer upserts in key order and bumps
// only dirty_version, so a re-mark never piles a second row behind the first.
//
// These two tests are the writer-side half of the verdict's failing-first
// order. Test (1) proves a re-mark of an in-flight key survives as one row
// with a bumped version and a preserved first-mark timestamp (the
// compare-and-delete settle of lane B relies on exactly this shape).
// Test (2) proves 150 production-path marks of one key collapse to one row.
// The deterministic deadlock interleave (test 3) and the settle half ride
// lane B; the existing live suites below stay the fence.

// dirtyFactRow reads one fact-kind dirty row for the package key.
func dirtyFactRow(h *keyedHarness, pkg, c0 string) (seq, version int64, captured time.Time) {
	h.t.Helper()
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact' AND package_id = $1 AND c0 = $2`, pkg, c0); got != 1 {
		h.t.Fatalf("dirty rows for (%s, %s) = %d, want exactly 1: repeated marks must collapse to one row", pkg, c0, got)
	}
	if err := h.db.QueryRowContext(h.ctx, `SELECT seq, dirty_version, captured_at
FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact' AND package_id = $1 AND c0 = $2`, pkg, c0).Scan(&seq, &version, &captured); err != nil {
		h.t.Fatalf("read collapsed dirty row: %v", err)
	}
	return seq, version, captured
}

// TestSupplyChainImpactWinnersDirtyReMarkSurvivesLive is failing-test-first (1):
// a mark committed for a key an in-flight tick already probed stays queued.
// The probe snapshot (seq, version) stands in for the tick's admitted copy:
// the re-mark must reuse the queued row (same seq, bumped version, first
// captured_at preserved) instead of appending a second row, so a
// version-checked settle leaves it queued for the next tick.
func TestSupplyChainImpactWinnersDirtyReMarkSurvivesLive(t *testing.T) {
	h := newKeyedHarness(t, "#10827 versioned-upsert survival proof")
	scope, gen := "vuln-intel://10827/survive", "vuln-intel://10827/survive/g1"
	h.seedScope(scope, gen)
	// The pointer flip queued one scope-kind row; it is not the capture
	// under test.
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)

	const pkg, cve = "pkg:npm/10827-survive", "CVE-2026-9001"
	first := keyedFinding(cve, pkg, 5)
	first.ObservedVersion = "1.0.0"
	h.write(scope, gen, first)
	seq, version, captured := dirtyFactRow(h, pkg, cve)
	if version != 1 {
		t.Fatalf("dirty_version after the first mark = %d, want 1", version)
	}

	// The in-flight tick admits (seq, version) here; the re-mark below
	// commits while that probe is outstanding.
	second := keyedFinding(cve, pkg, 60)
	second.ObservedVersion = "2.0.0"
	h.write(scope, gen, second)

	// The capture upsert and the retract upsert each bump the same queued
	// row once: no second row, same seq, version + 2.
	gotSeq, gotVersion, gotCaptured := dirtyFactRow(h, pkg, cve)
	if gotSeq != seq {
		t.Fatalf("collapsed row seq = %d, want the queued seq %d: the re-mark appended a new row", gotSeq, seq)
	}
	if want := version + 2; gotVersion != want {
		t.Fatalf("dirty_version after the re-mark = %d, want %d (capture bump plus retract bump)", gotVersion, want)
	}
	if !gotCaptured.Equal(captured) {
		t.Fatalf("captured_at moved from %v to %v: the upsert must preserve the first mark", captured, gotCaptured)
	}

	h.keyed()
	h.assertParity("after the surviving re-mark")
}

// TestSupplyChainImpactWinnersDirtyOneFiftyMarksCollapseLive is failing-test-first (2): one
// OSV mint wave re-marking one popular key 150 times must leave one dirty
// row, not 150. Every pass changes the observed version, so every pass
// captures (new digest) and retracts (tombstones the previous fact id); the
// first pass marks once, each later pass marks twice.
func TestSupplyChainImpactWinnersDirtyOneFiftyMarksCollapseLive(t *testing.T) {
	h := newKeyedHarness(t, "#10827 150-mark collapse proof")
	scope, gen := "vuln-intel://10827/collapse", "vuln-intel://10827/collapse/g1"
	h.seedScope(scope, gen)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)

	const pkg, cve = "pkg:npm/10827-collapse", "CVE-2026-9100"
	const passes = 150
	for i := 1; i <= passes; i++ {
		finding := keyedFinding(cve, pkg, i)
		finding.ObservedVersion = fmt.Sprintf("1.1.%d", i)
		h.write(scope, gen, finding)
	}

	// 1 mark on the first pass, 2 on each of the 149 that follow.
	const wantVersion = int64(1 + (passes-1)*2)
	_, gotVersion, _ := dirtyFactRow(h, pkg, cve)
	if gotVersion != wantVersion {
		t.Fatalf("dirty_version after %d marks = %d, want %d (one row bumped per mark)", passes, gotVersion, wantVersion)
	}

	h.keyed()
	h.assertParity("after the 150-mark collapse")
}
