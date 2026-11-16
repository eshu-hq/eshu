// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// retractedKeyedFinding is keyedFinding re-derived as advisory_retracted, the
// state a holder scope commits once it sees the CVE's source records rejected.
func retractedKeyedFinding(cve, pkg string, priority int) supplychaincore.SupplyChainImpactFinding {
	finding := keyedFinding(cve, pkg, priority)
	finding.Suppression = supplychaincore.SupplyChainSuppressionDecision{
		State:  supplychaincore.SupplyChainSuppressionStateAdvisoryRetracted,
		Reason: "advisory retracted by its source",
	}
	return finding
}

// stalePinWinner is the winners row the default findings view reads for one
// canonical key.
type stalePinWinner struct {
	factID, scopeID, state string
	score                  int
}

// stalePinWinnerOf reads the single winners row whose winner is one of the two
// candidate facts. The copies of one finding in two holder scopes share a
// canonical key, so exactly one row names one of them.
func (h *keyedHarness) stalePinWinnerOf(factA, factB string) stalePinWinner {
	h.t.Helper()
	var winner stalePinWinner
	if err := h.db.QueryRowContext(h.ctx, `
SELECT winner_fact_id, winner_scope_id, suppression_state, priority_score
FROM supply_chain_impact_canonical_winners
WHERE winner_fact_id IN ($1, $2)`, factA, factB).Scan(&winner.factID, &winner.scopeID, &winner.state, &winner.score); err != nil {
		h.t.Fatalf("read the winner of %s / %s: %v", factA, factB, err)
	}
	return winner
}

// TestSupplyChainImpactWinnersResweepStaleActiveCopyOutranksRetractedCopyLive
// is a characterization pin for #10765, not a fix. It records, on the real
// winners select and the real writer, that the default findings view can show a
// stale active copy of a finding while another holder scope has already
// re-derived the same finding as advisory_retracted:
//
//   - the stale copy scores higher: the stale active copy wins;
//   - the scores are equal: fact_id, a hash of identity, scope and generation,
//     decides, and the suppression state plays no part;
//   - the stale scope re-derives (what the holder fan-out reopens it to do):
//     the retracted copy wins and the key converges.
//
// The arbiter ruled this bounded behavior, not a defect to fix in the ordering:
// a retraction-aware tiebreak would let a stale retracted copy hide a restored
// real vulnerability (#9695 probe V2). The bound is the holder fan-out plus the
// winners audit backstop (docs/internal/evidence/9695b-holder-fanout.md). If a
// change makes this pin fail, it changed winner ordering, which needs its own
// ruling, an index and keyset review, and a contract class decision.
func TestSupplyChainImpactWinnersResweepStaleActiveCopyOutranksRetractedCopyLive(t *testing.T) {
	h := newKeyedHarness(t, "#10765 stale active copy pin")
	const (
		stale = "vuln-intel://10765/stale" // holds the stale active copy
		fresh = "vuln-intel://10765/fresh" // re-derived the retracted copy
		pkg   = "pkg:npm/widget"
	)
	h.seedScope(stale, stale+"/g1")
	h.seedScope(fresh, fresh+"/g1")

	// A pass is a replace-set, so each scope writes its whole finding set at
	// once. Key A: the stale copy scores higher. Keys B and C: equal scores,
	// with the retraction on opposite sides.
	const (
		cveHigher = "CVE-2026-10765"
		cveTieB   = "CVE-2026-10766"
		cveTieC   = "CVE-2026-10767"
	)
	h.write(stale, stale+"/g1",
		keyedFinding(cveHigher, pkg, 100), keyedFinding(cveTieB, pkg, 50), retractedKeyedFinding(cveTieC, pkg, 50))
	h.write(fresh, fresh+"/g1",
		retractedKeyedFinding(cveHigher, pkg, 40), retractedKeyedFinding(cveTieB, pkg, 50), keyedFinding(cveTieC, pkg, 50))
	h.keyed()

	t.Run("a higher scoring stale active copy wins over the retracted copy", func(t *testing.T) {
		winner := h.stalePinWinnerOf(h.findingFactID(stale, cveHigher), h.findingFactID(fresh, cveHigher))
		if winner.scopeID != stale || winner.state != "active" || winner.score != 100 {
			t.Fatalf("winner = %+v, want the stale active copy (scope %s, active, score 100)", winner, stale)
		}
		h.assertParity("stale active copy outranks retracted copy")
	})

	t.Run("an equal score is decided by fact_id and not by suppression state", func(t *testing.T) {
		for _, cve := range []string{cveTieB, cveTieC} {
			factStale, factFresh := h.findingFactID(stale, cve), h.findingFactID(fresh, cve)
			want := factStale
			if factFresh < factStale {
				want = factFresh
			}
			winner := h.stalePinWinnerOf(factStale, factFresh)
			if winner.factID != want || winner.score != 50 {
				t.Fatalf("%s: winner = %+v, want the copy with the lower fact_id %s at score 50", cve, winner, want)
			}
			t.Logf("%s: fact_id decided for %s (state %s)", cve, winner.scopeID, winner.state)
		}
		h.assertParity("equal scores decided by fact_id")
	})

	t.Run("swapping which side is retracted at an equal score keeps the same winning fact", func(t *testing.T) {
		factStale, factFresh := h.findingFactID(stale, cveTieB), h.findingFactID(fresh, cveTieB)
		before := h.stalePinWinnerOf(factStale, factFresh)
		// Same generations and fact ids, retraction moved to the other scope:
		// only the suppression state of each copy changes.
		h.write(stale, stale+"/g1",
			keyedFinding(cveHigher, pkg, 100), retractedKeyedFinding(cveTieB, pkg, 50), retractedKeyedFinding(cveTieC, pkg, 50))
		h.write(fresh, fresh+"/g1",
			retractedKeyedFinding(cveHigher, pkg, 40), keyedFinding(cveTieB, pkg, 50), keyedFinding(cveTieC, pkg, 50))
		h.keyed()
		after := h.stalePinWinnerOf(factStale, factFresh)
		if after.factID != before.factID {
			t.Fatalf("winning fact changed from %s to %s when only the retraction moved sides", before.factID, after.factID)
		}
		if after.state == before.state {
			t.Fatalf("winner state stayed %q after the retraction moved sides, want it to flip with the copy", after.state)
		}
		t.Logf("winning fact %s was %s, is now %s", after.factID, before.state, after.state)
		h.assertParity("retraction moved sides at an equal score")
	})

	t.Run("once the stale scope re-derives the copy as retracted the retraction wins", func(t *testing.T) {
		// The holder fan-out reopens the stale scope; its pass re-derives the
		// copy from current evidence and commits it retracted. Same generation
		// and fact id: the digest changed, so the writer rewrites the row.
		result := h.write(stale, stale+"/g1",
			retractedKeyedFinding(cveHigher, pkg, 100), keyedFinding(cveTieB, pkg, 50), retractedKeyedFinding(cveTieC, pkg, 50))
		// The pass reads the stored state through the production query and
		// reports what it repaired: the higher-scoring copy left active, and
		// the tie copy B left retracted (the previous subtest retracted it).
		if want := (supplychaincore.StaleCopyRepairs{ActiveToRetracted: 1, RetractedToActive: 1}); result.StaleCopyRepairs != want {
			t.Fatalf("StaleCopyRepairs = %+v, want %+v", result.StaleCopyRepairs, want)
		}
		h.keyed()
		winner := h.stalePinWinnerOf(h.findingFactID(stale, cveHigher), h.findingFactID(fresh, cveHigher))
		if winner.state != "advisory_retracted" {
			t.Fatalf("winner = %+v after the stale scope re-derived, want advisory_retracted", winner)
		}
		h.assertParity("converged after the stale scope re-derived")
	})
}
