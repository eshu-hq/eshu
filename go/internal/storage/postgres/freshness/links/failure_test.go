// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"testing"
	"time"
)

// TestBackoffDoublesFromThirtySecondsToTheCap pins ruling 8.10's backoff with
// literal durations, never Backoff itself as the oracle (review F2): a flat
// or wrongly based backoff must fail here.
func TestBackoffDoublesFromThirtySecondsToTheCap(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  time.Duration
	}{
		{0, 30 * time.Second},
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 120 * time.Second},
		{4, 240 * time.Second},
		{5, 480 * time.Second},
		{6, 960 * time.Second},
		{7, 30 * time.Minute},
		{10, 30 * time.Minute},
		{64, 30 * time.Minute},
	} {
		if got := Backoff(tc.count); got != tc.want {
			t.Errorf("Backoff(%d) = %s, want %s", tc.count, got, tc.want)
		}
	}
}
