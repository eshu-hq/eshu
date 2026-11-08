// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDecideSuccessorWriteOnceCells pins the write-once arm
// (eshu-hq/eshu#10037): a write-once successor over a live (pending or
// active) prior always refuses, ahead of every other arm including a
// trusted-deletion attestation, because no generation of a write-once
// scope may supersede another. A prior that only failed does not count,
// so the first storable generation still lands.
func TestDecideSuccessorWriteOnceCells(t *testing.T) {
	t.Parallel()

	cells := []struct {
		name       string
		in         SuccessorGuardInput
		wantRefuse bool
		wantRule   string
	}{
		{
			name:       "write-once over a live prior refuses even at full size",
			in:         SuccessorGuardInput{WriteOnce: true, PriorFound: true, PriorLive: true, PriorCollector: 10, SuccessorFacts: 100},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleWriteOnce,
		},
		{
			name:       "write-once beats a trusted-deletion attestation",
			in:         SuccessorGuardInput{WriteOnce: true, PriorFound: true, PriorLive: true, PriorCollector: 10, SuccessorFacts: 0, TrustedDeletion: true},
			wantRefuse: true,
			wantRule:   telemetry.SuccessorGuardRuleWriteOnce,
		},
		{
			name:     "write-once with a failed-only prior stores",
			in:       SuccessorGuardInput{WriteOnce: true, PriorFound: true, PriorLive: false, PriorCollector: 10, SuccessorFacts: 5},
			wantRule: telemetry.SuccessorGuardRuleCollectorRatio,
		},
		{
			name:     "write-once with no prior stores",
			in:       SuccessorGuardInput{WriteOnce: true, SuccessorFacts: 5},
			wantRule: telemetry.SuccessorGuardRuleNoPrior,
		},
		{
			name:     "ordinary successor over a live prior keeps the ratio rule",
			in:       SuccessorGuardInput{PriorFound: true, PriorLive: true, PriorCollector: 10, SuccessorFacts: 100},
			wantRule: telemetry.SuccessorGuardRuleCollectorRatio,
		},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			got := DecideSuccessor(cell.in)
			if got.Refuse != cell.wantRefuse || got.Rule != cell.wantRule {
				t.Fatalf("DecideSuccessor(%+v) = {Refuse:%t Rule:%q}, want {Refuse:%t Rule:%q}",
					cell.in, got.Refuse, got.Rule, cell.wantRefuse, cell.wantRule)
			}
		})
	}
}
