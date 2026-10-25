// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestClaimDepthMaxOverlap pins the sweep predicate behind the #8725 overlap
// gate: attempt windows ([AttemptElapsed, DoneElapsed]) report how many
// grants were simultaneously in flight. Back-to-back serial grants share only
// an endpoint and report 1, so a serialized grant path cannot pass a gate
// set above 1.
func TestClaimDepthMaxOverlap(t *testing.T) {
	window := func(start, end time.Duration) claimDepthRecord {
		return claimDepthRecord{AttemptElapsed: start, DoneElapsed: end}
	}
	cases := []struct {
		name    string
		records []claimDepthRecord
		want    int
	}{
		{"no records report zero", nil, 0},
		{"single window reports one", []claimDepthRecord{window(0, 10)}, 1},
		{"disjoint windows report one", []claimDepthRecord{window(0, 10), window(20, 30)}, 1},
		{"touching endpoints report one", []claimDepthRecord{window(0, 10), window(10, 20)}, 1},
		{"nested windows report two", []claimDepthRecord{window(0, 30), window(10, 20)}, 2},
		{"identical windows report two", []claimDepthRecord{window(5, 15), window(5, 15)}, 2},
		{"three staggered windows report three", []claimDepthRecord{window(0, 20), window(5, 25), window(10, 15)}, 3},
		{"zero-length window contributes nothing", []claimDepthRecord{window(0, 10), window(10, 10)}, 1},
		{"inverted window contributes nothing", []claimDepthRecord{window(20, 10)}, 0},
		{"peak counts only the true overlap", []claimDepthRecord{window(0, 100), window(10, 20), window(30, 40)}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimDepthMaxOverlap(tc.records); got != tc.want {
				t.Fatalf("claimDepthMaxOverlap(%v) = %d, want %d", tc.records, got, tc.want)
			}
		})
	}
}

// TestClaimDepthDrainSpeedupOK pins the load-insensitive anti-serialization
// bound (#8725 P1 disposition): a parallel drain must achieve at least half
// the enrolled claimers' worth of speedup over a single-claimer calibration
// drain of the same backlog shape. The bound derives from the test's own
// parameters, not from a measured wall-clock floor, so hosted runner noise
// cancels out of the ratio.
func TestClaimDepthDrainSpeedupOK(t *testing.T) {
	cases := []struct {
		name     string
		single   float64
		parallel float64
		claimers int
		want     bool
	}{
		{"eight-way full speedup holds", 8.0, 1.0, 8, true},
		{"half parallelism holds at the boundary", 4.0, 1.0, 8, true},
		{"below half parallelism fails", 3.9, 1.0, 8, false},
		{"serialized single effective claimer fails", 1.0, 1.0, 8, false},
		{"slower than single fails", 0.5, 1.0, 8, false},
		{"two claimers need full sharing", 2.0, 1.0, 2, true},
		{"two claimers below half fails", 0.9, 1.0, 2, false},
		{"degenerate single claimer proves nothing", 1.0, 1.0, 1, false},
		{"zero claimers fails", 1.0, 1.0, 0, false},
		{"zero single duration fails", 0.0, 1.0, 8, false},
		{"zero parallel duration fails", 8.0, 0.0, 8, false},
		{"negative single duration fails", -1.0, 1.0, 8, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimDepthDrainSpeedupOK(tc.single, tc.parallel, tc.claimers); got != tc.want {
				t.Fatalf("claimDepthDrainSpeedupOK(single=%.1f, parallel=%.1f, claimers=%d) = %v, want %v",
					tc.single, tc.parallel, tc.claimers, got, tc.want)
			}
		})
	}
}
