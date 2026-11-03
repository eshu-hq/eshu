// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import "testing"

// TestSupplyChainImpactWinnersResweepAuditRepairsAMissedCaptureLive proves the
// backstop of decision 3 of the #9805 ruling. A winner made stale by a change
// that left no dirty-log row is found by the audit, counted once, queued for
// the next keyed tick, and repaired there; a second audit then finds nothing.
func TestSupplyChainImpactWinnersResweepAuditRepairsAMissedCaptureLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 audit backstop proof")
	scopeA, _ := keyedSeedSettled(h)

	clean, err := h.store.AuditWinners(h.ctx)
	if err != nil || clean.Skipped || clean.Repairs != 0 {
		t.Fatalf("audit of a settled table = %+v, %v; want zero repairs", clean, err)
	}

	// A missed capture: change a fact's priority with raw SQL, which no capture
	// path sees.
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '99')
WHERE scope_id = $1 AND payload->>'cve_id' = 'CVE-2026-2002'`, scopeA)
	audit, err := h.store.AuditWinners(h.ctx)
	if err != nil || audit.Skipped {
		t.Fatalf("audit = %+v, %v", audit, err)
	}
	if audit.Repairs != 1 {
		t.Fatalf("audit repairs = %d, want exactly the one stale key", audit.Repairs)
	}
	if depth := h.dirtyDepth(); depth != 1 {
		t.Fatalf("dirty log depth after the audit = %d, want the one repaired key queued", depth)
	}
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE source = 'audit'`); got != 1 {
		t.Fatalf("audit-sourced dirty rows = %d, want 1", got)
	}
	h.keyed()
	h.assertParity("after the audit repair")
	again, err := h.store.AuditWinners(h.ctx)
	if err != nil || again.Repairs != 0 {
		t.Fatalf("second audit = %+v, %v; want zero repairs once the keyed tick repaired the key", again, err)
	}
}

// TestSupplyChainImpactWinnersResweepAuditFindsAMissedDeleteLive covers the other
// direction: a winner whose facts vanished with no capture.
func TestSupplyChainImpactWinnersResweepAuditFindsAMissedDeleteLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 audit delete proof")
	scopeA, _ := keyedSeedSettled(h)
	h.exec(`UPDATE fact_records SET is_tombstone = TRUE WHERE scope_id = $1 AND payload->>'cve_id' = 'CVE-2026-2002'`, scopeA)
	audit, err := h.store.AuditWinners(h.ctx)
	if err != nil || audit.Repairs != 1 {
		t.Fatalf("audit = %+v, %v; want one repair for the missed delete", audit, err)
	}
	h.keyed()
	h.assertParity("after the delete repair")
}

// TestSupplyChainImpactWinnersResweepAuditDoesNotCountPendingKeysLive pins the
// steady-state contract: a change that is captured and still waiting for its
// keyed tick is not a capture hole, so the audit must not count it.
func TestSupplyChainImpactWinnersResweepAuditDoesNotCountPendingKeysLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 audit pending proof")
	scopeA, scopeB := keyedSeedSettled(h)

	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 5))
	h.addGeneration(scopeB, scopeB+"/g2", "pending")
	h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2003", "pkg:npm/z", 1))
	h.activate(scopeB, scopeB+"/g1", scopeB+"/g2") // leaves scope rows pending
	pending := h.dirtyDepth()
	audit, err := h.store.AuditWinners(h.ctx)
	if err != nil || audit.Repairs != 0 {
		t.Fatalf("audit over pending changes = %+v, %v; want zero repairs", audit, err)
	}
	if after := h.dirtyDepth(); after != pending {
		t.Fatalf("dirty log depth moved from %d to %d: the audit appended keys that were already pending", pending, after)
	}
	h.keyed()
	h.assertParity("after pending keys")
}
