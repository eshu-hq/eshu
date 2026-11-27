// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
)

// epssDatedFinding returns the finding shape an EPSS rescan derives: the same
// selected score with the given score date.
func epssDatedFinding(score, date string) SupplyChainImpactFinding {
	finding := changedTestFinding("CVE-2026-0001")
	finding.EPSSProbability = score
	finding.EPSSPercentile = "0.96"
	finding.EPSSScoreDate = date
	return finding
}

// TestContentDigestIgnoresEPSSScoreDateOnly pins #11598 Fix 2: the selected
// EPSS score date differs on every rescan without the finding changing (once
// the score is unchanged), so a date-only refresh must leave the content
// digest — and with it the row-write judgement, the winners capture, and the
// fan-out — untouched. The score itself stays digest-covered.
func TestContentDigestIgnoresEPSSScoreDateOnly(t *testing.T) {
	t.Parallel()

	write := changedTestWrite()
	base := supplyChainImpactContentDigest(write, epssDatedFinding("0.78", "2026-05-16"))
	if base == "" {
		t.Fatal("content digest is empty")
	}
	if got := supplyChainImpactContentDigest(write, epssDatedFinding("0.78", "2026-05-17")); got != base {
		t.Fatalf("date-only digest = %q, want the base digest %q: a score-date-only rescan reads as a change", got, base)
	}
	if got := supplyChainImpactContentDigest(write, epssDatedFinding("0.12", "2026-05-17")); got == base {
		t.Fatal("content digest did not move with the EPSS score: the score stays digest-covered")
	}
}

// TestDateOnlyRefreshReportsNoChangeAndCapturesNothing pins the fleet-wide
// consequence of the digest fix: the first pass of a new generation whose only
// difference is the EPSS score date reports no changed CVE and stages no
// winner capture, while the freshly written fact rows still carry the new
// date. The list route serves the stored winner payload (it reads
// supply_chain_impact_canonical_winners, pinned by the query package's
// winners-table route test and the winner-row decode test), so the served date
// goes stale on a date-only refresh: accepted staleness for a provenance
// metadata field while the score is unchanged, never a silent rewrite.
func TestDateOnlyRefreshReportsNoChangeAndCapturesNothing(t *testing.T) {
	t.Parallel()

	prior := changedTestWrite(epssDatedFinding("0.78", "2026-05-16"))
	prior.GenerationID = "generation-prior"
	next := changedTestWrite(epssDatedFinding("0.78", "2026-05-17"))
	next.GenerationID = "generation-next"

	stableKey := supplyChainImpactStableFactKey(next, next.Findings[0])
	if want := supplyChainImpactStableFactKey(prior, prior.Findings[0]); stableKey != want {
		t.Fatalf("stable key moved across generations: %q != %q", stableKey, want)
	}
	rows := []changedRow{{
		factID:          "fact-next",
		stableKey:       stableKey,
		digest:          supplyChainImpactContentDigest(next, next.Findings[0]),
		cveID:           "CVE-2026-0001",
		evidenceFactIDs: next.Findings[0].EvidenceFactIDs,
	}}
	stored := []storedFinding{{
		factID:          "fact-prior",
		generationID:    "generation-prior",
		stableKey:       stableKey,
		digest:          supplyChainImpactContentDigest(prior, prior.Findings[0]),
		cveID:           "CVE-2026-0001",
		fencingToken:    5,
		evidenceFactIDs: prior.Findings[0].EvidenceFactIDs,
	}}
	if got := changedCVEIDs(rows, stored, "generation-next", 10, true); len(got) != 0 {
		t.Fatalf("changedCVEIDs = %v, want none: a score-date-only refresh changes nothing", got)
	}
	if got := dirtyIdentifiersToCapture(rows, stored, "generation-next"); len(got) != 0 {
		t.Fatalf("dirtyIdentifiersToCapture = %v, want none: no winner is recaptured, so the stored winner keeps the old date", got)
	}
	// The facts move while the winners do not: the new rows persist the new
	// date, which is what a later content change will serve.
	if got := supplyChainImpactTypedPayload(next, next.Findings[0]).Provenance["selected_epss_score_date"]; got != "2026-05-17" {
		t.Fatalf("new fact provenance date = %v, want 2026-05-17: persisted provenance still carries the new date", got)
	}
}
