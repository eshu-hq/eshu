// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// Fixture for eshu-hq/eshu#9878 Phase 2 item 2.3: how KEV and EPSS changes
// reach existing findings.
//
// Today-behavior recorder only: it seeds a KEV-listed CVE on an owned
// package, runs the KEV scope build, then lowers the EPSS score and runs
// again, and projects the durable winners row each time (stable canonical
// key, per-scope fact id, payload priority_score/kev/epss). It then probes
// the three rescore hypotheses:
//
// (a) duplicate per-scope copies: the same logical finding built under two
// scopes yields two fact rows under one canonical key;
// (b) lowered-EPSS masking: whether a lowered score reaches the finding,
// which preferEPSSObservation decides by score_date, never by magnitude;
// (c) KEV rerun cadence: KEV lands in the finding synchronously at build
// time, while the winners read model only moves on maintainer resweep
// (default 30s cadence).
//
// Zone rule: this fixture never enters zone D. It builds findings in memory
// and emulates the documented winner order (priority_score DESC,
// has_payload_finding_id DESC, fact_id ASC, no score_date tiebreak) over the
// projected rows; it does not touch the Postgres winners store.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const rescore9878CVE = "CVE-2026-9001"

// rescore9878BaseEnvelopes seeds one owned-package CVE with a KEV listing and
// one EPSS observation. The EPSS score_date is the promotion input:
// preferEPSSObservation ranks newest score_date first, so a lowered score
// only reaches the finding when its score_date is newer. cvss tunes whether
// the summed contributions hit the 100-point clamp (7.5 saturates with KEV;
// 3.0 stays below it so EPSS movement is visible in the score).
func rescore9878BaseEnvelopes(cvss float64, epssProb, epssPct, scoreDate string, withKEV bool) []facts.Envelope {
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-9878", rescore9878CVE, cvss),
		vulnerabilityAffectedPackageFact("affected-9878", rescore9878CVE, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-9878", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		vulnerabilityEPSSFact("epss-9878", rescore9878CVE, epssProb, epssPct),
	}
	envelopes[3].Payload["score_date"] = scoreDate
	if withKEV {
		envelopes = append(envelopes, vulnerabilityKEVFact("kev-9878", rescore9878CVE))
	}
	return envelopes
}

// rescore9878WinnersRow is the core-visible projection of one durable winners
// row: the canonical key the resweep dedups on, the per-scope fact id it
// orders by, and the payload signals the order reads.
type rescore9878WinnersRow struct {
	canonicalKey      string
	factID            string
	hasPayloadFinding bool
	priorityScore     int
	priorityBucket    string
	knownExploited    bool
	epssProbability   string
	epssScoreDate     string
	evidenceFactIDs   []string
	reasonCodes       []string
}

func projectRescore9878WinnersRow(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) rescore9878WinnersRow {
	payload := supplyChainImpactPayload(write, finding)
	_, hasFinding := payload["finding_id"]
	return rescore9878WinnersRow{
		canonicalKey:      supplyChainImpactStableFactKey(write, finding),
		factID:            supplyChainImpactFactID(write, finding),
		hasPayloadFinding: hasFinding,
		priorityScore:     finding.PriorityScore,
		priorityBucket:    string(finding.PriorityBucket),
		knownExploited:    finding.KnownExploited,
		epssProbability:   finding.EPSSProbability,
		epssScoreDate:     finding.EPSSScoreDate,
		evidenceFactIDs:   append([]string(nil), finding.EvidenceFactIDs...),
		reasonCodes:       append([]string(nil), finding.PriorityReasonCodes...),
	}
}

func (r rescore9878WinnersRow) log(t *testing.T, label string) {
	t.Helper()
	t.Logf("%s canonical_key=%q fact_id=%q priority=%d bucket=%s kev=%v epss=%s/%s evidence=%q reasons=%q",
		label, r.canonicalKey, r.factID, r.priorityScore, r.priorityBucket,
		r.knownExploited, r.epssProbability, r.epssScoreDate,
		strings.Join(r.evidenceFactIDs, ","), strings.Join(r.reasonCodes, ","))
}

// pickRescore9878Winner emulates the shipped winner order over projected rows:
// priority_score DESC, has_payload_finding_id DESC, fact_id ASC. There is no
// score_date tiebreak: a stale higher-score copy can only win through its
// priority_score, never through recency.
func pickRescore9878Winner(rows []rescore9878WinnersRow) rescore9878WinnersRow {
	winner := rows[0]
	for _, row := range rows[1:] {
		switch {
		case row.priorityScore != winner.priorityScore:
			if row.priorityScore > winner.priorityScore {
				winner = row
			}
		case row.hasPayloadFinding != winner.hasPayloadFinding:
			if row.hasPayloadFinding {
				winner = row
			}
		case row.factID < winner.factID:
			winner = row
		}
	}
	return winner
}

func mustRescore9878Finding(t *testing.T, envelopes []facts.Envelope) SupplyChainImpactFinding {
	t.Helper()
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}

func TestKEVEPSSRescoreWinnersRow9878(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{ScopeID: "scope-kev-9878", GenerationID: "gen-1"}

	// Phase 1: KEV scope run with high EPSS. Observed 2026-11-03: the summed
	// contributions (cvss high 35 + epss_high 20 + percentile_high 20 +
	// cisa_kev 30 + exact 15 + owned repo 10 + fixed 5 - missing 5 = 130)
	// hit the 100-point clamp, so this realistic high-severity KEV case
	// records saturation, not the raw sum.
	phase1 := mustRescore9878Finding(t, rescore9878BaseEnvelopes(7.5, "0.71", "0.98", "2026-05-16", true))
	row1 := projectRescore9878WinnersRow(write, phase1)
	row1.log(t, "phase1-kev-high-epss")
	if !phase1.KnownExploited {
		t.Fatalf("phase1 KnownExploited = false, want KEV-listed CVE flagged")
	}
	if phase1.EPSSProbability != "0.71" || phase1.EPSSScoreDate != "2026-05-16" {
		t.Fatalf("phase1 EPSS = %q/%q, want 0.71/2026-05-16", phase1.EPSSProbability, phase1.EPSSScoreDate)
	}
	if !slices.Contains(phase1.PriorityReasonCodes, "cisa_kev") {
		t.Fatalf("phase1 reason codes = %q, want cisa_kev (+30)", phase1.PriorityReasonCodes)
	}
	if phase1.PriorityScore != 100 || phase1.PriorityBucket != "critical" {
		t.Fatalf("phase1 priority = %d/%q, want saturated 100/critical", phase1.PriorityScore, phase1.PriorityBucket)
	}

	// Phase 2: EPSS lowered with a NEWER score_date. The lowered score reaches
	// the finding (selection ranks newest date first) but the priority stays
	// clamped at 100: the EPSS drop only swaps reason codes (epss_high ->
	// epss_elevated, percentile_high dropped). Same scope+generation also
	// yields the same fact_id: a same-scope rerun updates the row in place.
	phase2 := mustRescore9878Finding(t, rescore9878BaseEnvelopes(7.5, "0.12", "0.32", "2026-05-17", true))
	row2 := projectRescore9878WinnersRow(write, phase2)
	row2.log(t, "phase2-kev-lowered-epss-newer-date")
	if phase2.EPSSProbability != "0.12" || phase2.EPSSScoreDate != "2026-05-17" {
		t.Fatalf("phase2 EPSS = %q/%q, want lowered 0.12/2026-05-17", phase2.EPSSProbability, phase2.EPSSScoreDate)
	}
	if !phase2.KnownExploited || !slices.Contains(phase2.PriorityReasonCodes, "cisa_kev") {
		t.Fatalf("phase2 lost KEV signal: kev=%v codes=%q", phase2.KnownExploited, phase2.PriorityReasonCodes)
	}
	if phase2.PriorityScore != 100 || phase2.PriorityBucket != "critical" {
		t.Fatalf("phase2 priority = %d/%q, want clamped 100/critical (EPSS drop hidden by clamp)",
			phase2.PriorityScore, phase2.PriorityBucket)
	}
	if slices.Contains(phase2.PriorityReasonCodes, "epss_high") ||
		slices.Contains(phase2.PriorityReasonCodes, "epss_percentile_high") {
		t.Fatalf("phase2 codes = %q, want epss_high/epss_percentile_high gone", phase2.PriorityReasonCodes)
	}
	if row2.canonicalKey != row1.canonicalKey {
		t.Fatalf("canonical key moved across rescore:\nphase1 %q\nphase2 %q", row1.canonicalKey, row2.canonicalKey)
	}
	if row2.factID != row1.factID {
		t.Fatalf("same-scope rescore fact_id moved:\nphase1 %q\nphase2 %q", row1.factID, row2.factID)
	}

	// Phase 2m (movement, below the clamp): CVSS 3.0 keeps every phase under
	// 100 so the lowered EPSS is visible in the score. Observed 2026-11-03:
	// 98 -> 70 (cvss low 3 + epss 20/12 + percentile 20/0 + cisa_kev 30 +
	// exact 15 + owned repo 10 + fixed 5 - missing 5).
	move1 := mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.71", "0.98", "2026-05-16", true))
	move2 := mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.12", "0.32", "2026-05-17", true))
	projectRescore9878WinnersRow(write, move1).log(t, "phase2m-high-epss")
	projectRescore9878WinnersRow(write, move2).log(t, "phase2m-lowered-epss")
	if move1.PriorityScore != 98 || move2.PriorityScore != 70 {
		t.Fatalf("movement priorities = %d -> %d, want 98 -> 70", move1.PriorityScore, move2.PriorityScore)
	}

	// Phase 2b (stale lowered feed): the lowered score carries an OLDER
	// score_date than the selected 2026-05-16 observation. Hypothesis (b):
	// the older higher copy masks it and the finding keeps 0.71.
	stale := rescore9878BaseEnvelopes(7.5, "0.71", "0.98", "2026-05-16", true)
	lowered := vulnerabilityEPSSFact("epss-9878-lowered", rescore9878CVE, "0.05", "0.10")
	lowered.Payload["score_date"] = "2026-05-15"
	stale = append(stale, lowered)
	phaseStale := mustRescore9878Finding(t, stale)
	rowStale := projectRescore9878WinnersRow(write, phaseStale)
	rowStale.log(t, "phase2b-stale-lowered-epss")
	if phaseStale.EPSSProbability != "0.71" {
		t.Fatalf("stale lowered EPSS reached the finding: got %q, want masked 0.71", phaseStale.EPSSProbability)
	}
}

func TestKEVEPSSRescoreDuplicateScopeCopies9878(t *testing.T) {
	t.Parallel()

	// Hypothesis (a): the same logical finding published from two scopes is
	// two fact rows under one canonical key. Fact identity carries
	// scope_id+generation_id; the stable key does not.
	scopeA := SupplyChainImpactWrite{ScopeID: "scope-a-9878", GenerationID: "gen-1"}
	scopeB := SupplyChainImpactWrite{ScopeID: "scope-b-9878", GenerationID: "gen-2"}
	findingA := mustRescore9878Finding(t, rescore9878BaseEnvelopes(7.5, "0.71", "0.98", "2026-05-16", true))
	findingB := mustRescore9878Finding(t, rescore9878BaseEnvelopes(7.5, "0.71", "0.98", "2026-05-16", true))
	rowA := projectRescore9878WinnersRow(scopeA, findingA)
	rowB := projectRescore9878WinnersRow(scopeB, findingB)
	rowA.log(t, "scope-a")
	rowB.log(t, "scope-b")
	if rowA.factID == rowB.factID {
		t.Fatalf("duplicate scopes share fact_id %q, want one row per scope", rowA.factID)
	}
	if rowA.canonicalKey != rowB.canonicalKey {
		t.Fatalf("canonical keys differ across scopes:\nA %q\nB %q", rowA.canonicalKey, rowB.canonicalKey)
	}

	// Same score in both scopes: the order has no recency tiebreak, so the
	// smaller fact_id wins deterministically.
	tied := pickRescore9878Winner([]rescore9878WinnersRow{rowA, rowB})
	wantTied := rowA.factID
	if rowB.factID < wantTied {
		wantTied = rowB.factID
	}
	if tied.factID != wantTied {
		t.Fatalf("tied winner fact_id = %q, want smaller %q", tied.factID, wantTied)
	}
	t.Logf("tied winner: fact_id=%q priority=%d", tied.factID, tied.priorityScore)

	// Hypothesis (b) at read level, below the clamp so scores can differ:
	// scope B rescored with lowered EPSS (newer date, so the build reflects
	// it: 98 -> 70) while scope A still carries the older higher copy. The
	// higher copy wins the winner row on score alone; no score_date tiebreak
	// can save the fresher lowered copy.
	highA := mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.71", "0.98", "2026-05-16", true))
	rowHighA := projectRescore9878WinnersRow(scopeA, highA)
	rowHighA.log(t, "scope-a-high-below-clamp")
	loweredB := mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.12", "0.32", "2026-05-17", true))
	rowBLowered := projectRescore9878WinnersRow(scopeB, loweredB)
	rowBLowered.log(t, "scope-b-lowered")
	if rowBLowered.priorityScore >= rowHighA.priorityScore {
		t.Fatalf("lowered copy priority=%d, want below older copy=%d", rowBLowered.priorityScore, rowHighA.priorityScore)
	}
	readWinner := pickRescore9878Winner([]rescore9878WinnersRow{rowHighA, rowBLowered})
	if readWinner.factID != rowHighA.factID {
		t.Fatalf("read winner fact_id = %q, want older higher copy %q", readWinner.factID, rowHighA.factID)
	}
	t.Logf("masked winner: fact_id=%q epss=%s (older higher copy masks lowered rescore)",
		readWinner.factID, readWinner.epssProbability)
}

func TestKEVEPSSRescoreRerunCadence9878(t *testing.T) {
	t.Parallel()

	// Hypothesis (c): KEV is synchronous at build time -- no resweep needed
	// for the finding to carry it -- while the winners read model only moves
	// when the maintainer resweeps.
	finding := mustRescore9878Finding(t, rescore9878BaseEnvelopes(7.5, "0.71", "0.98", "2026-05-16", true))
	if !finding.KnownExploited {
		t.Fatalf("KEV missing from fresh build without any resweep")
	}
	t.Logf("kev synchronous at build: priority=%d bucket=%s", finding.PriorityScore, finding.PriorityBucket)

	probe := SupplyChainImpactWinnersMaintainer{}
	if got := probe.interval(); got != 30*time.Second {
		t.Fatalf("maintainer interval = %v, want 30s default cadence", got)
	}
	rebuilder := &fakeWinnersRebuilder{}
	leases := &fakeWinnersLeaseManager{claim: true}
	maintainer := SupplyChainImpactWinnersMaintainer{Rebuilder: rebuilder, LeaseManager: leases}
	rebuilt, err := maintainer.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if !rebuilt || rebuilder.count() != 1 {
		t.Fatalf("rebuilt=%v resweeps=%d, want one committed resweep per tick", rebuilt, rebuilder.count())
	}
	t.Logf("resweep cadence: interval=30s one RunOnce tick commits exactly one resweep; KEV visibility at read waits for that tick")
}

func BenchmarkKEVEPSSRescoreBuild9878(b *testing.B) {
	for _, scale := range []struct {
		name  string
		count int
	}{
		{name: "1x_10findings", count: 10},
		{name: "5x_50findings", count: 50},
		{name: "10x_100findings", count: 100},
	} {
		b.Run(scale.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var envelopes []facts.Envelope
				for n := 0; n < scale.count; n++ {
					cve := fmt.Sprintf("CVE-2026-91%02d", n)
					pkg := fmt.Sprintf("pkg:npm/mod-%d", n)
					envelopes = append(envelopes,
						vulnerabilityCVEFact(fmt.Sprintf("cve-%d", n), cve, 7.5),
						vulnerabilityAffectedPackageFact(fmt.Sprintf("affected-%d", n), cve, pkg, "npm", fmt.Sprintf("mod-%d", n), "1.2.3", "1.3.0"),
						packageConsumptionFactWithRange(fmt.Sprintf("consume-%d", n), pkg, testImpactRepositoryID, "1.2.3"),
						vulnerabilityEPSSFact(fmt.Sprintf("epss-%d", n), cve, "0.71", "0.98"),
						vulnerabilityKEVFact(fmt.Sprintf("kev-%d", n), cve),
					)
				}
				if findings := BuildSupplyChainImpactFindings(envelopes); len(findings) != scale.count {
					b.Fatalf("len(findings) = %d, want %d", len(findings), scale.count)
				}
			}
		})
	}
}
