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
