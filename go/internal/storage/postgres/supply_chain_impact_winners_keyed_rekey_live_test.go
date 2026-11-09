// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// L4 keyed-path convergence proofs for re-key on advisory group merge/split
// (issue #10408, criterion 3.3). A merge retires group ids two ways: the
// committer's replace-set retraction tombstones the dead row in the same
// generation (the TOMBSTONE route, whose retract statement appends the dirty
// row), and a new generation supersedes the old one through the pointer move
// (the GENERATION-SUPERSESSION route, whose migration-272 trigger captures
// the scope rows the keyed tick expands). Both tests settle the table, retire
// a dead key through one route, run one keyed tick (never the full rebuild),
// and require zero winner rows under the dead key, a drained dirty log, a
// table equal to a fresh recompute, and a full-recompute audit reporting zero
// repairs.

// rekeyKeyedFinding is a keyed-harness finding carrying an advisory-group
// member set, the way a merged pass persists the survivor's group.
func rekeyKeyedFinding(cve, pkg string, priority int, members ...string) supplychaincore.SupplyChainImpactFinding {
	finding := keyedFinding(cve, pkg, priority)
	finding.AdvisoryGroupMembers = members
	return finding
}

// deadWinnerCount returns the stored winners naming a retired group id.
func deadWinnerCount(h *keyedHarness, cve string) int {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, cve)
}

// TestSupplyChainImpactWinnersResweepRekeyTombstoneDeadKeyConvergesLive retires
// a dead group id through the same-generation retraction: the committer
// merges onto the survivor with the merged member set and the dead row is
// tombstoned. One keyed tick must delete the dead winner, drain the log, and
// leave the audit with nothing to repair.
func TestSupplyChainImpactWinnersResweepRekeyTombstoneDeadKeyConvergesLive(t *testing.T) {
	h := newKeyedHarness(t, "#10408 L4 tombstone dead-key proof")
	scopeA, _ := keyedSeedSettled(h)

	const dead = "CVE-2026-2002"
	if got := deadWinnerCount(h, dead); got != 1 {
		t.Fatalf("settled winners naming %s = %d, want 1: the dead key must be live before the merge retires it", dead, got)
	}

	h.write(scopeA, scopeA+"/g1",
		rekeyKeyedFinding("CVE-2026-2001", "pkg:npm/x", 8, "CVE-2026-2001", dead),
		rekeyKeyedFinding("CVE-2026-2003", "pkg:npm/y", 5, "CVE-2026-2003"))
	if got := deadWinnerCount(h, dead); got != 1 {
		t.Fatalf("winners naming %s before the keyed tick = %d, want 1 (still stale: only the tick repairs it)", dead, got)
	}

	result := h.keyed()
	if result.Deleted == 0 {
		t.Fatalf("keyed tick = %+v, want the dead winner deleted", result)
	}
	if got := deadWinnerCount(h, dead); got != 0 {
		t.Fatalf("winners naming %s after one keyed tick = %d, want 0", dead, got)
	}
	h.assertParity("tombstone re-key dead key")
	if audit, err := h.store.AuditWinners(h.ctx); err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit after the tombstone re-key = %+v, %v; want zero repairs", audit, err)
	}
}

// TestSupplyChainImpactWinnersResweepRekeySupersessionDeadKeyConvergesLive
// retires a dead group id through generation supersession: a new generation
// drops the scope's unique key and the pointer move activates it. The keyed
// tick expands the trigger's scope rows to the old generation's identifiers,
// so one keyed tick must delete the dead winner with no fact-row capture for
// it.
func TestSupplyChainImpactWinnersResweepRekeySupersessionDeadKeyConvergesLive(t *testing.T) {
	h := newKeyedHarness(t, "#10408 L4 supersession dead-key proof")
	_, scopeB := keyedSeedSettled(h)

	const dead = "CVE-2026-2003"
	h.write(scopeB, scopeB+"/g1",
		keyedFinding("CVE-2026-2001", "pkg:npm/x", 8),
		keyedFinding(dead, "pkg:npm/z", 1))
	h.keyed()
	h.assertParity("unique key seeded")
	if got := deadWinnerCount(h, dead); got != 1 {
		t.Fatalf("settled winners naming %s = %d, want 1: the dead key must be live before supersession retires it", dead, got)
	}

	h.addGeneration(scopeB, scopeB+"/g2", "pending")
	h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2001", "pkg:npm/x", 8))
	h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")

	result := h.keyed()
	if result.Deleted == 0 {
		t.Fatalf("keyed tick = %+v, want the superseded winner deleted", result)
	}
	if got := deadWinnerCount(h, dead); got != 0 {
		t.Fatalf("winners naming %s after one keyed tick = %d, want 0", dead, got)
	}
	h.assertParity("supersession re-key dead key")
	if audit, err := h.store.AuditWinners(h.ctx); err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit after the supersession re-key = %+v, %v; want zero repairs", audit, err)
	}
}
