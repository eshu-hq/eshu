// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
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

// TestQuantizeEPSSPercentileFloorBoundaries pins the #11598 Option C 0.01-floor
// percentile quantization and priority threshold invariance: raw 9-decimal
// percentiles floor cleanly to two decimals before priority scoring and payload
// encoding, while preferEPSSObservation continues to tie-break on raw strings.
func TestQuantizeEPSSPercentileFloorBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw      string
		wantPct  string
		wantCode string
	}{
		{raw: "", wantPct: "", wantCode: ""},
		{raw: "0", wantPct: "0.00", wantCode: ""},
		{raw: "0.277810000", wantPct: "0.27", wantCode: ""},
		{raw: "0.277860000", wantPct: "0.27", wantCode: ""},
		{raw: "0.29", wantPct: "0.29", wantCode: ""},
		{raw: "0.57", wantPct: "0.57", wantCode: ""},
		{raw: "0.799999", wantPct: "0.79", wantCode: ""},
		{raw: "0.800000", wantPct: "0.80", wantCode: "epss_percentile_elevated"},
		{raw: " 0.805 ", wantPct: "0.80", wantCode: "epss_percentile_elevated"},
		{raw: "0.949999999", wantPct: "0.94", wantCode: "epss_percentile_elevated"},
		{raw: "0.950000000", wantPct: "0.95", wantCode: "epss_percentile_high"},
		{raw: "9.5e-1", wantPct: "0.95", wantCode: "epss_percentile_high"},
		{raw: "0.99012", wantPct: "0.99", wantCode: "epss_percentile_high"},
		{raw: "1.0", wantPct: "1.00", wantCode: "epss_percentile_high"},
		// Out-of-range, non-decimal, and hostile inputs pass through trimmed
		// unchanged without panicking or exhausting memory in big.Rat.
		{raw: "-0.05", wantPct: "-0.05", wantCode: ""},
		{raw: "1.1", wantPct: "1.1", wantCode: "epss_percentile_high"},
		{raw: "NaN", wantPct: "NaN", wantCode: ""},
		{raw: "Inf", wantPct: "Inf", wantCode: "epss_percentile_high"},
		{raw: "1/2", wantPct: "1/2", wantCode: ""},
		{raw: "0x1p-1", wantPct: "0x1p-1", wantCode: ""},
		{raw: "0.1_2", wantPct: "0.1_2", wantCode: ""},
		{raw: "1e-100000000", wantPct: "1e-100000000", wantCode: ""},
	}
	for _, tc := range cases {
		f := changedTestFinding("CVE-2026-8723")
		applyRiskSignals(&f, supplychainmodel.RiskSignals{
			EPSSProbability: "0.004180000",
			EPSSPercentile:  tc.raw,
			EPSSScoreDate:   "2026-11-26",
			EPSSFactID:      "epss-fact-1",
			EPSSSelected:    true,
		})
		f = withSupplyChainImpactPriority(f)
		if f.EPSSPercentile != tc.wantPct {
			t.Errorf("raw=%q: EPSSPercentile = %q, want %q", tc.raw, f.EPSSPercentile, tc.wantPct)
		}
		hasHigh := slices.Contains(f.PriorityReasonCodes, "epss_percentile_high")
		hasElevated := slices.Contains(f.PriorityReasonCodes, "epss_percentile_elevated")
		switch tc.wantCode {
		case "":
			if hasHigh || hasElevated {
				t.Errorf("raw=%q: got unexpected percentile reason codes %v", tc.raw, f.PriorityReasonCodes)
			}
		case "epss_percentile_elevated":
			if !hasElevated || hasHigh {
				t.Errorf("raw=%q: reason codes = %v, want epss_percentile_elevated only", tc.raw, f.PriorityReasonCodes)
			}
		case "epss_percentile_high":
			if !hasHigh || hasElevated {
				t.Errorf("raw=%q: reason codes = %v, want epss_percentile_high only", tc.raw, f.PriorityReasonCodes)
			}
		}
	}

	// preferEPSSObservation must still compare raw values before finding derivation.
	cur := supplychainmodel.RiskSignals{EPSSSelected: true, EPSSScoreDate: "2026-11-26", EPSSFactID: "f1", EPSSProbability: "0.004180000", EPSSPercentile: "0.277810000"}
	cand := supplychainmodel.RiskSignals{EPSSSelected: true, EPSSScoreDate: "2026-11-26", EPSSFactID: "f1", EPSSProbability: "0.004180000", EPSSPercentile: "0.277860000"}
	if !preferEPSSObservation(cand, cur) {
		t.Fatal("preferEPSSObservation must still break ties on raw EPSSPercentile")
	}
}

// TestEPSSSubCentDriftAndNewFactIDCapturesNothing pins the #11598 Option C
// combined invariant: a daily EPSS refresh with unchanged probability, sub-0.01
// percentile drift (0.27781 -> 0.27786 or 0.95111 -> 0.95119), newer score_date,
// and a newly minted EPSS fact_id reports zero changed CVEs and stages zero
// winners dirty keys, while a real probability jump or cross-0.01 percentile
// move still triggers change + capture.
func TestEPSSSubCentDriftAndNewFactIDCapturesNothing(t *testing.T) {
	t.Parallel()

	build := func(prob, pct, date, epssFactID string) SupplyChainImpactFinding {
		f := changedTestFinding("CVE-2026-8723")
		applyRiskSignals(&f, supplychainmodel.RiskSignals{
			EPSSProbability: prob,
			EPSSPercentile:  pct,
			EPSSScoreDate:   date,
			EPSSFactID:      epssFactID,
			EPSSSelected:    true,
		})
		return withSupplyChainImpactPriority(f)
	}

	for _, pair := range []struct {
		name     string
		prob     string
		pctPrior string
		pctNext  string
	}{
		{name: "low percentile sub-cent drift", prob: "0.004180000", pctPrior: "0.277810000", pctNext: "0.277860000"},
		{name: "high percentile sub-cent drift (covers PriorityContributions)", prob: "0.780000000", pctPrior: "0.951110000", pctNext: "0.951190000"},
	} {
		prior := changedTestWrite(build(pair.prob, pair.pctPrior, "2026-11-25", "epss-25"))
		prior.GenerationID = "gen-25"
		next := changedTestWrite(build(pair.prob, pair.pctNext, "2026-11-26", "epss-26"))
		next.GenerationID = "gen-26"

		stableKey := supplyChainImpactStableFactKey(next, next.Findings[0])
		ident := WinnersDirtyIdentifier{PackageID: "pkg:npm/qs", RepositoryID: "repo-1", C0: "CVE-2026-8723"}
		rows := []changedRow{{
			factID:          "fact-26",
			stableKey:       stableKey,
			digest:          supplyChainImpactContentDigest(next, next.Findings[0]),
			cveID:           "CVE-2026-8723",
			ident:           ident,
			evidenceFactIDs: next.Findings[0].EvidenceFactIDs,
		}}
		stored := []storedFinding{{
			factID:          "fact-25",
			generationID:    "gen-25",
			stableKey:       stableKey,
			digest:          supplyChainImpactContentDigest(prior, prior.Findings[0]),
			cveID:           "CVE-2026-8723",
			fencingToken:    5,
			evidenceFactIDs: prior.Findings[0].EvidenceFactIDs,
		}}
		if got := changedCVEIDs(rows, stored, "gen-26", 10, true); len(got) != 0 {
			t.Errorf("%s: changedCVEIDs = %v, want none", pair.name, got)
		}
		if got := dirtyIdentifiersToCapture(rows, stored, "gen-26"); len(got) != 0 {
			t.Errorf("%s: dirtyIdentifiersToCapture = %v, want none", pair.name, got)
		}

		// Real probability jump or cross-0.01 percentile move must stage.
		moved := changedTestWrite(build("0.550000000", pair.pctNext, "2026-11-26", "epss-26"))
		moved.GenerationID = "gen-26"
		movedRows := []changedRow{{
			factID:          "fact-26",
			stableKey:       stableKey,
			digest:          supplyChainImpactContentDigest(moved, moved.Findings[0]),
			cveID:           "CVE-2026-8723",
			ident:           ident,
			evidenceFactIDs: moved.Findings[0].EvidenceFactIDs,
		}}
		if got := dirtyIdentifiersToCapture(movedRows, stored, "gen-26"); len(got) != 1 {
			t.Errorf("%s: real probability move dirtyIdentifiersToCapture = %v, want 1", pair.name, got)
		}
	}

	// Cross-0.01 percentile-only move at constant probability must report
	// changed + stage (threshold crossing 0.799999 -> 0.800000).
	constProb := "0.004180000"
	pctPrior := changedTestWrite(build(constProb, "0.799999", "2026-11-25", "epss-25"))
	pctPrior.GenerationID = "gen-25"
	pctNext := changedTestWrite(build(constProb, "0.800000", "2026-11-26", "epss-26"))
	pctNext.GenerationID = "gen-26"
	stableKey := supplyChainImpactStableFactKey(pctNext, pctNext.Findings[0])
	ident := WinnersDirtyIdentifier{PackageID: "pkg:npm/qs", RepositoryID: "repo-1", C0: "CVE-2026-8723"}
	pctRows := []changedRow{{
		factID:          "fact-26",
		stableKey:       stableKey,
		digest:          supplyChainImpactContentDigest(pctNext, pctNext.Findings[0]),
		cveID:           "CVE-2026-8723",
		ident:           ident,
		evidenceFactIDs: pctNext.Findings[0].EvidenceFactIDs,
	}}
	pctStored := []storedFinding{{
		factID:          "fact-25",
		generationID:    "gen-25",
		stableKey:       stableKey,
		digest:          supplyChainImpactContentDigest(pctPrior, pctPrior.Findings[0]),
		cveID:           "CVE-2026-8723",
		fencingToken:    5,
		evidenceFactIDs: pctPrior.Findings[0].EvidenceFactIDs,
	}}
	if got := changedCVEIDs(pctRows, pctStored, "gen-26", 10, true); len(got) != 1 {
		t.Errorf("cross-0.01 percentile-only move changedCVEIDs = %v, want 1", got)
	}
	if got := dirtyIdentifiersToCapture(pctRows, pctStored, "gen-26"); len(got) != 1 {
		t.Errorf("cross-0.01 percentile-only move dirtyIdentifiersToCapture = %v, want 1", got)
	}
}
