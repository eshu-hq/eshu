// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

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
