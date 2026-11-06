// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// lastFactIDWins mirrors the Postgres fact upsert at equal fencing tokens: of
// several envelopes sharing a FactID, the last one is the one stored.
func lastFactIDWins(in []facts.Envelope) []facts.Envelope {
	last := map[string]int{}
	for i, envelope := range in {
		last[envelope.FactID] = i
	}
	out := make([]facts.Envelope, 0, len(in))
	for i, envelope := range in {
		if last[envelope.FactID] == i {
			out = append(out, envelope)
		}
	}
	return out
}

// TestNVDCollectorOutputGivesOrderIndependentFindings pushes one NVD record,
// whose CPE match repeats across configurations, through the real collector
// function and the reducer (with the storage layer's last-write-wins fact
// dedupe applied). The finding count must not depend on configuration order,
// and must be 0 only when every occurrence is negated (#9696).
func TestNVDCollectorOutputGivesOrderIndependentFindings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		fixture      string
		wantFindings int
	}{
		{name: "plain then negated", fixture: "built-negate-plain-negated-configuration", wantFindings: 1},
		{name: "negated then plain", fixture: "built-negate-negated-then-plain", wantFindings: 1},
		{name: "plain then negated node", fixture: "built-negate-plain-negated-node", wantFindings: 1},
		{name: "negated node then plain", fixture: "built-negate-negated-node-then-plain", wantFindings: 1},
		{name: "every occurrence negated", fixture: "built-negate-both-negated", wantFindings: 0},
		{name: "every occurrence negated, reversed", fixture: "built-negate-both-negated-reversed", wantFindings: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Frozen NVD converter output (eshu-hq/eshu#9999): the satellite
			// owns the converter now, so the test loads the frozen shapes.
			emitted := convertedOSVFixture(t, tc.fixture, false)

			stored := lastFactIDWins(emitted)
			stored = append(stored, ownedSBOMProductEvidence()...)
			findings := BuildSupplyChainImpactFindings(stored)

			if got := len(findings); got != tc.wantFindings {
				t.Fatalf("len(findings) = %d, want %d", got, tc.wantFindings)
			}
		})
	}
}
