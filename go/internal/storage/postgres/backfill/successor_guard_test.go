// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDecideSuccessorCells pins every arm of the #7926 destructive-successor
// backstop as sized by #8750: the prior is counted on collector rows only,
// and a delta successor is sized by its projected file union against its
// anchor. Each cell names the deciding rule so a silent arm swap is caught.
func TestDecideSuccessorCells(t *testing.T) {
	t.Parallel()

	cells := []struct {
		name       string
		in         SuccessorGuardInput
		wantRefuse bool
		wantProbe  bool
		wantRule   string
		wantBound  int
	}{
		{
			name:     "trusted deletion stores an empty successor",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 100, TrustedDeletion: true},
			wantRule: telemetry.SuccessorGuardRuleTrustedDeletion,
		},
		{
			name:     "trusted deletion beats the orphan refusal",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 100, PriorOrphan: true, TrustedDeletion: true},
			wantRule: telemetry.SuccessorGuardRuleTrustedDeletion,
		},
		{
			name:     "no prior stores",
			in:       SuccessorGuardInput{SuccessorFacts: 10},
			wantRule: telemetry.SuccessorGuardRuleNoPrior,
		},
		{
			name:     "prior with zero collector rows stores a non-empty successor",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 0, SuccessorFacts: 5},
			wantRule: telemetry.SuccessorGuardRuleNoPrior,
		},
		{
			name:     "prior with zero collector rows stores an empty successor",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 0, SuccessorFacts: 0},
			wantRule: telemetry.SuccessorGuardRuleNoPrior,
		},
		{
			name:     "reducer-heavy prior: 11360 against 15231 collector rows stores",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 15231, SuccessorFacts: 11360},
			wantRule: telemetry.SuccessorGuardRuleCollectorRatio,
		},
		{
			name:       "reducer-heavy prior: a real shrink below half the collector rows refuses",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 15231, SuccessorFacts: 7000},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleCollectorRatio,
		},
		{
			name:     "exactly half the collector rows stores",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 100, SuccessorFacts: 50},
			wantRule: telemetry.SuccessorGuardRuleCollectorRatio,
		},
		{
			name:       "one under half the collector rows refuses",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 100, SuccessorFacts: 49},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleCollectorRatio,
		},
		{
			name:       "empty successor over a content-bearing prior refuses",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 10},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleEmpty,
		},
		{
			name:       "orphan prior under an empty successor refuses even with zero collector rows",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 0, PriorOrphan: true},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleOrphanFallback,
		},
		{
			name:       "orphan prior under an empty successor refuses",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 40, PriorOrphan: true},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleOrphanFallback,
		},
		{
			name:       "orphan prior sized on its own rows refuses a sharp full shrink and names the orphan rule",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 40, PriorOrphan: true, SuccessorFacts: 10},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleOrphanFallback,
		},
		{
			name:     "Delta false falls to the collector ratio even with delta-shaped counts",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 1000, SuccessorFacts: 41, Delta: false, AnchorFiles: 1000, TouchedOrLive: 1},
			wantRule: telemetry.SuccessorGuardRuleCollectorRatio,
			// 41*2 < 1000, so this is refused: the fallback sizes a
			// mismatched generation as a full.
			wantRefuse: true,
		},
		{
			name:       "empty delta refuses",
			in:         SuccessorGuardInput{PriorFound: true, PriorCollector: 1000, Delta: true, AnchorFiles: 100},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleEmpty,
		},
		{
			name:     "delta with no anchor stores so the projector arms the reindex watermark",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 1000, SuccessorFacts: 41, Delta: true, AnchorMissing: true},
			wantRule: telemetry.SuccessorGuardRuleAnchorMissing,
		},
		{
			name:      "ordinary delta (41 facts over a 175k prior) stores on the bound with no probe",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 175858, SuccessorFacts: 41, Delta: true, AnchorFiles: 2000, TouchedOrLive: 41, LiveFiles: 41},
			wantRule:  telemetry.SuccessorGuardRuleFileUnionBound,
			wantBound: 2000,
		},
		{
			name:      "delta with no deletes keeps the whole anchor and needs no probe",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 12, Delta: true, AnchorFiles: 100, TouchedOrLive: 5, LiveFiles: 5},
			wantRule:  telemetry.SuccessorGuardRuleFileUnionBound,
			wantBound: 100,
		},
		{
			name:      "delta at exactly half the anchor by the bound stores without a probe",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 12, Delta: true, AnchorFiles: 100, TouchedOrLive: 50, LiveFiles: 0},
			wantRule:  telemetry.SuccessorGuardRuleFileUnionBound,
			wantBound: 50,
		},
		{
			name:      "mass delete fails the bound and asks for the exact probe",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 80, Delta: true, AnchorFiles: 100, TouchedOrLive: 80, LiveFiles: 0},
			wantProbe: true,
			wantRule:  telemetry.SuccessorGuardRuleFileUnionExact,
			wantBound: 20,
		},
		{
			name:      "one path past half fails the bound",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 80, Delta: true, AnchorFiles: 100, TouchedOrLive: 51, LiveFiles: 0},
			wantProbe: true,
			wantRule:  telemetry.SuccessorGuardRuleFileUnionExact,
			wantBound: 49,
		},
		{
			name:      "rename of more than a third of the files fails the bound and probes",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 100, Delta: true, AnchorFiles: 90, TouchedOrLive: 100, LiveFiles: 50},
			wantProbe: true,
			wantRule:  telemetry.SuccessorGuardRuleFileUnionExact,
			wantBound: 40,
		},
		{
			name:      "anchor with zero files stores on the bound",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 500, SuccessorFacts: 10, Delta: true, AnchorFiles: 0, TouchedOrLive: 10, LiveFiles: 10},
			wantRule:  telemetry.SuccessorGuardRuleFileUnionBound,
			wantBound: 0,
		},
		{
			name:      "tiny repo: deleting two of three files fails the bound and probes",
			in:        SuccessorGuardInput{PriorFound: true, PriorCollector: 9, SuccessorFacts: 2, Delta: true, AnchorFiles: 3, TouchedOrLive: 2, LiveFiles: 0},
			wantProbe: true,
			wantRule:  telemetry.SuccessorGuardRuleFileUnionExact,
			wantBound: 1,
		},
		{
			name:     "delta trusted deletion stores before any anchor arithmetic",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 500, Delta: true, AnchorFiles: 100, TouchedOrLive: 100, TrustedDeletion: true},
			wantRule: telemetry.SuccessorGuardRuleTrustedDeletion,
		},
		{
			name:     "delta over a prior with no collector rows stores",
			in:       SuccessorGuardInput{PriorFound: true, PriorCollector: 0, SuccessorFacts: 3, Delta: true, AnchorFiles: 100, TouchedOrLive: 100},
			wantRule: telemetry.SuccessorGuardRuleNoPrior,
		},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			got := DecideSuccessor(cell.in)
			if got.Refuse != cell.wantRefuse {
				t.Errorf("Refuse = %v, want %v (rule %q)", got.Refuse, cell.wantRefuse, got.Rule)
			}
			if got.NeedsProbe != cell.wantProbe {
				t.Errorf("NeedsProbe = %v, want %v", got.NeedsProbe, cell.wantProbe)
			}
			if got.Rule != cell.wantRule {
				t.Errorf("Rule = %q, want %q", got.Rule, cell.wantRule)
			}
			if got.UnionLowerBound != cell.wantBound {
				t.Errorf("UnionLowerBound = %d, want %d", got.UnionLowerBound, cell.wantBound)
			}
			if got.NeedsProbe && got.Refuse {
				t.Error("a decision that asks for a probe must not also refuse: the bound only under-estimates")
			}
		})
	}
}

// TestDecideDeltaFileUnionCells pins the exact-probe verdict: the union keeps
// every untouched anchor file and adds the delta's live files, and refuses
// only when it falls under half the anchor.
func TestDecideDeltaFileUnionCells(t *testing.T) {
	t.Parallel()

	cells := []struct {
		name        string
		anchorFiles int
		touched     int
		liveFiles   int
		wantRefuse  bool
		wantUnion   int
	}{
		{name: "mass delete refuses", anchorFiles: 100, touched: 80, liveFiles: 0, wantRefuse: true, wantUnion: 20},
		{name: "deleting only never-indexed paths removes nothing and stores", anchorFiles: 100, touched: 0, liveFiles: 0, wantUnion: 100},
		{name: "rename nets to zero and stores", anchorFiles: 90, touched: 50, liveFiles: 50, wantUnion: 90},
		{name: "exactly half stores", anchorFiles: 10, touched: 5, liveFiles: 0, wantUnion: 5},
		{name: "one past half refuses", anchorFiles: 10, touched: 6, liveFiles: 0, wantRefuse: true, wantUnion: 4},
		{name: "tiny repo deleting two of three refuses", anchorFiles: 3, touched: 2, liveFiles: 0, wantRefuse: true, wantUnion: 1},
		{name: "tiny repo deleting one of three stores", anchorFiles: 3, touched: 1, liveFiles: 0, wantUnion: 2},
		{name: "live additions offset deletions", anchorFiles: 10, touched: 8, liveFiles: 4, wantUnion: 6},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			refuse, union := DecideDeltaFileUnion(cell.anchorFiles, cell.touched, cell.liveFiles)
			if refuse != cell.wantRefuse {
				t.Errorf("refuse = %v, want %v", refuse, cell.wantRefuse)
			}
			if union != cell.wantUnion {
				t.Errorf("union = %d, want %d", union, cell.wantUnion)
			}
		})
	}
}

// TestDecideSuccessorBoundNeverRefusesWhatTheProbeStores sweeps a grid and
// proves the zero-query bound is conservative: whenever the bound stores
// (no probe), the exact union decision for the best-case overlap would also
// store, so skipping the probe never lets a destructive delta through that
// the probe would have refused.
func TestDecideSuccessorBoundNeverRefusesWhatTheProbeStores(t *testing.T) {
	t.Parallel()

	for anchor := 0; anchor <= 24; anchor++ {
		for touchedOrLive := 0; touchedOrLive <= 30; touchedOrLive++ {
			for live := 0; live <= touchedOrLive; live++ {
				decision := DecideSuccessor(SuccessorGuardInput{
					PriorFound: true, PriorCollector: 100, SuccessorFacts: 5,
					Delta: true, AnchorFiles: anchor, TouchedOrLive: touchedOrLive, LiveFiles: live,
				})
				if decision.NeedsProbe {
					continue
				}
				// Worst case for the union: every touched-or-live path is an
				// anchor file, bounded by the anchor's own size.
				worstTouched := touchedOrLive
				if worstTouched > anchor {
					worstTouched = anchor
				}
				refuse, union := DecideDeltaFileUnion(anchor, worstTouched, live)
				if anchor > 0 && refuse {
					t.Fatalf("bound stored anchor=%d touchedOrLive=%d live=%d but worst-case union %d would refuse", anchor, touchedOrLive, live, union)
				}
			}
		}
	}
}
