// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import (
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	interval := 10 * time.Minute

	tests := []struct {
		name string
		obs  Observation
		want bool
	}{
		{name: "evaluated now", obs: Observation{EvaluatedAt: now, EvaluationInterval: interval}, want: true},
		{name: "exactly three intervals old", obs: Observation{EvaluatedAt: now.Add(-30 * time.Minute), EvaluationInterval: interval}, want: true},
		{name: "older than three intervals", obs: Observation{EvaluatedAt: now.Add(-30*time.Minute - time.Second), EvaluationInterval: interval}, want: false},
		{name: "evaluated after the read clock", obs: Observation{EvaluatedAt: now.Add(time.Minute), EvaluationInterval: interval}, want: true},
		{name: "zero interval is never live", obs: Observation{EvaluatedAt: now}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Live(tt.obs, now); got != tt.want {
				t.Fatalf("Live() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfirmed(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	interval := 5 * time.Minute

	tests := []struct {
		name string
		obs  Observation
		want bool
	}{
		{
			name: "two cycles spanning the interval",
			obs:  Observation{State: StateNotListed, UnlistedCycleCount: 2, FirstUnlistedAt: first, EvaluatedAt: first.Add(interval), EvaluationInterval: interval},
			want: true,
		},
		{
			name: "one cycle is pending",
			obs:  Observation{State: StateNotListed, UnlistedCycleCount: 1, FirstUnlistedAt: first, EvaluatedAt: first.Add(time.Hour), EvaluationInterval: interval},
			want: false,
		},
		{
			name: "two cycles inside one interval are pending",
			obs:  Observation{State: StateNotListed, UnlistedCycleCount: 2, FirstUnlistedAt: first, EvaluatedAt: first.Add(interval - time.Second), EvaluationInterval: interval},
			want: false,
		},
		{
			name: "archived exclusion is positive evidence, not confirmation",
			obs:  Observation{State: StateArchivedExcluded, UnlistedCycleCount: 5, EvaluatedAt: first.Add(time.Hour), EvaluationInterval: interval},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Confirmed(tt.obs); got != tt.want {
				t.Fatalf("Confirmed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExcluded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	interval := 5 * time.Minute
	confirmed := Observation{State: StateNotListed, UnlistedCycleCount: 2, FirstUnlistedAt: now.Add(-interval), EvaluatedAt: now, EvaluationInterval: interval}
	pending := Observation{State: StateNotListed, UnlistedCycleCount: 1, FirstUnlistedAt: now, EvaluatedAt: now, EvaluationInterval: interval}

	for _, tt := range []struct {
		obs  Observation
		want bool
	}{
		{Observation{State: StateSelected}, false},
		{Observation{State: StateArchivedExcluded}, true},
		{Observation{State: StateRuleExcluded}, true},
		{confirmed, true},
		{pending, false},
		{Observation{State: "unknown_state"}, false},
	} {
		if got := Excluded(tt.obs); got != tt.want {
			t.Fatalf("Excluded(%+v) = %v, want %v", tt.obs, got, tt.want)
		}
	}
}
