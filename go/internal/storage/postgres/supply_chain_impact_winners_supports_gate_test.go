// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestWinnersSupportsGateEmbedsTheReadinessPredicateAndComparesDurableStamps pins
// the gate statement (#10880 PR-C1 R2): it embeds the pinned supports readiness
// predicate byte for byte, so the maintainer is gated by exactly what gates the
// writer and the readers, and it decides FullNeeded from the two durable rows only,
// the supports marker's completion stamp and the winners watermark.
func TestWinnersSupportsGateEmbedsTheReadinessPredicateAndComparesDurableStamps(t *testing.T) {
	t.Parallel()

	q := selectSupplyChainImpactWinnersSupportsGateSQL
	if !strings.Contains(q, supplyChainImpactSupportsReadySQL) {
		t.Fatal("the gate statement does not embed supplyChainImpactSupportsReadySQL byte for byte")
	}
	for name, want := range map[string]string{
		"marker completion stamp": "SELECT completed_at FROM supply_chain_impact_finding_supports_backfill WHERE marker_name = $1",
		"winners watermark":       "SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1",
		"never-built watermark":   "'-infinity'::timestamptz",
		"gated on readiness":      "ready AND",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("the gate statement lacks %s: %q", name, want)
		}
	}
	if strings.Contains(q, "fact_records") {
		t.Error("the gate statement reads fact_records")
	}
}

func TestWinnersSupportsGateReadsBothAnswersAndBindsTheMarker(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		row               []any
		wantReady, wantFn bool
	}{
		"not ready":               {[]any{false, false}, false, false},
		"ready, marker older":     {[]any{true, false}, true, false},
		"ready, marker newer":     {[]any{true, true}, true, true},
		"not ready with a stamp":  {[]any{false, true}, false, true},
		"ready false full needed": {[]any{true, false}, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeQueryer{responses: []fakeRows{{rows: [][]any{tc.row}}}}
			gate, err := readWinnersSupportsGate(context.Background(), q)
			if err != nil {
				t.Fatalf("readWinnersSupportsGate() error = %v", err)
			}
			if gate.Ready != tc.wantReady || gate.FullNeeded != tc.wantFn {
				t.Fatalf("gate = %+v, want ready %v full needed %v", gate, tc.wantReady, tc.wantFn)
			}
			if len(q.args) != 1 || len(q.args[0]) != 2 ||
				q.args[0][0] != SupplyChainImpactSupportsBackfillMarker || q.args[0][1] != SupplyChainImpactSupportsContractVersion {
				t.Fatalf("bind arguments = %v, want the marker name then the contract version", q.args)
			}
		})
	}

	// No row at all is an error, never a ready table.
	empty := &fakeQueryer{responses: []fakeRows{{}}}
	if _, err := readWinnersSupportsGate(context.Background(), empty); err == nil {
		t.Fatal("an empty gate read was accepted as an answer")
	}
}
