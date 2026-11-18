// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
)

// Writer-side proofs for eshu-hq/eshu#10837 direction C (hot-key
// coalescing). The staging queue collapses repeated marks of one recompute
// unit at tick time: every capture owner stages one row per mark with no
// arbiter, and the in-tick collapse restores the version math. A re-mark
// stages another row; the collapse, not the writer, combines them.
//
// These two tests are the writer-side half of the direction-C failing-first
// order. Test (1) proves a re-mark staged for an in-flight key survives the
// settle: the collapse bumps the queued log row past the admitted version,
// so the version recheck leaves it queued with its first-mark timestamp.
// Test (2) proves 150 production-path marks of one key stage as 150 rows
// and collapse to one log row at the exact version. The deterministic
// deadlock interleave and the settle half live in the staging collapse gate
// file; the existing live suites stay the fence.

// stagedFactRow reads the staged fact-kind rows for the package key: under
// direction C every mark stages its own row, so the count pins the marks.
func stagedFactRow(h *keyedHarness, pkg, c0 string) int {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'fact' AND package_id = $1 AND c0 = $2`, pkg, c0)
}

// TestSupplyChainImpactWinnersDirtyReMarkSurvivesLive is failing-test-first (1):
// a mark staged for a key an in-flight tick already probed stays queued.
// The probe snapshot (seq, version) stands in for the tick's admitted copy:
// the staged re-mark collapses into the queued row (bumped version, first
// captured_at preserved), so the version-checked settle leaves the key
// queued for the next tick.
func TestSupplyChainImpactWinnersDirtyReMarkSurvivesLive(t *testing.T) {
	h := newKeyedHarness(t, "#10837 staging survival proof")
	scope, gen := "vuln-intel://10837/survive", "vuln-intel://10837/survive/g1"
	h.seedScope(scope, gen)
	// The pointer flip staged scope-kind rows; they are not the capture
	// under test.
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)

	const pkg, cve = "pkg:npm/10837-survive", "CVE-2026-9001"
	first := keyedFinding(cve, pkg, 5)
	first.ObservedVersion = "1.0.0"
	h.write(scope, gen, first)
	// One capture mark plus one retract mark stage as two rows: staging
	// combines nothing, it only queues.
	if got := stagedFactRow(h, pkg, cve); got != 1 {
		t.Fatalf("staged rows after the first mark = %d, want 1 (capture; nothing to retract yet)", got)
	}
	r1 := h.keyed()
	// The tick consumed exactly the 1 staged mark: the collapse wrote one
	// version-1 row and the settle drained it.
	if r1.Identifiers != 1 {
		t.Fatalf("first tick consumed %d marks, want the 1 staged mark", r1.Identifiers)
	}

	// A re-mark of the settled key stages 2 fresh rows; the next tick
	// consumes exactly those, so no mark is lost or doubled.
	second := keyedFinding(cve, pkg, 60)
	second.ObservedVersion = "2.0.0"
	h.write(scope, gen, second)
	if got := stagedFactRow(h, pkg, cve); got != 2 {
		t.Fatalf("staged rows after the re-mark = %d, want 2 (capture plus retract)", got)
	}
	r2 := h.keyed()
	if r2.Identifiers != 2 {
		t.Fatalf("second tick consumed %d marks, want the 2 staged re-marks", r2.Identifiers)
	}

	h.assertParity("after the surviving re-mark")
}

// TestSupplyChainImpactWinnersDirtyOneFiftyMarksCollapseLive is failing-test-first (2): one
// OSV mint wave re-marking one popular key 150 times stages one row per
// mark. Every pass changes the observed version, so every pass captures (new
// digest) and retracts (tombstones the previous fact id); the first pass
// stages 1 mark, each later pass stages 2. The tick collapse combines them
// to one log row at the exact version and the settle drains it.
func TestSupplyChainImpactWinnersDirtyOneFiftyMarksCollapseLive(t *testing.T) {
	h := newKeyedHarness(t, "#10837 150-mark collapse proof")
	scope, gen := "vuln-intel://10837/collapse", "vuln-intel://10837/collapse/g1"
	h.seedScope(scope, gen)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)

	const pkg, cve = "pkg:npm/10837-collapse", "CVE-2026-9100"
	const passes = 150
	for i := 1; i <= passes; i++ {
		finding := keyedFinding(cve, pkg, i)
		finding.ObservedVersion = fmt.Sprintf("1.1.%d", i)
		h.write(scope, gen, finding)
	}

	// 1 mark on the first pass, 2 on each of the 149 that follow.
	const wantStaged = int64(1 + (passes-1)*2)
	if got := stagedFactRow(h, pkg, cve); int64(got) != wantStaged {
		t.Fatalf("staged rows after %d marks = %d, want %d (one staged row per mark)", passes, got, wantStaged)
	}

	r := h.keyed()
	// The tick consumed every staged mark: the collapse combined them to one
	// version-299 row and the settle drained it.
	if int64(r.Identifiers) != wantStaged {
		t.Fatalf("tick consumed %d marks, want the %d staged marks", r.Identifiers, wantStaged)
	}
	h.assertParity("after the 150-mark collapse")
}
