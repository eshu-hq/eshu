// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package affected

import "testing"

// TestShouldEmitRefresh pins the ACK emit gate: CanonicalWrites must be
// positive, and an explicit zero affected-repo signal suppresses the event
// while an absent signal fails open for unwired producers.
func TestShouldEmitRefresh(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		writes  int
		signals map[string]float64
		want    bool
	}{
		{"writes without signal fail open", 3, nil, true},
		{"writes with empty signals fail open", 3, map[string]float64{}, true},
		{"writes with positive signal emit", 3, map[string]float64{RefreshAffectedReposSignal: 2}, true},
		{"writes with explicit zero suppress", 3, map[string]float64{RefreshAffectedReposSignal: 0}, false},
		{"no writes with positive signal suppress", 0, map[string]float64{RefreshAffectedReposSignal: 2}, false},
		{"no writes without signal suppress", 0, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldEmitRefresh(tc.writes, tc.signals); got != tc.want {
				t.Errorf("ShouldEmitRefresh(%d, %v) = %v, want %v", tc.writes, tc.signals, got, tc.want)
			}
		})
	}
}
