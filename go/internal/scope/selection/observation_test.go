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
	window := 48 * time.Hour

	tests := []struct {
		name string
		obs  Observation
		want bool
	}{
		{name: "evaluated now", obs: Observation{EvaluatedAt: now, LivenessWindow: window}, want: true},
		{name: "20h gap inside a 48h window", obs: Observation{EvaluatedAt: now.Add(-20 * time.Hour), LivenessWindow: window}, want: true},
		{name: "exactly the window old", obs: Observation{EvaluatedAt: now.Add(-window), LivenessWindow: window}, want: true},
		{name: "one microsecond past the window", obs: Observation{EvaluatedAt: now.Add(-window - time.Microsecond), LivenessWindow: window}, want: false},
		{name: "50h gap past a 48h window", obs: Observation{EvaluatedAt: now.Add(-50 * time.Hour), LivenessWindow: window}, want: false},
		{name: "evaluated after the read clock", obs: Observation{EvaluatedAt: now.Add(time.Minute), LivenessWindow: window}, want: true},
		{name: "zero window is never live", obs: Observation{EvaluatedAt: now}, want: false},
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

	since := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	row := func(state State, cycles int, span time.Duration) Observation {
		return Observation{State: state, StateCycleCount: cycles, StateSince: since, EvaluatedAt: since.Add(span), LivenessWindow: 48 * time.Hour}
	}

	tests := []struct {
		name string
		obs  Observation
		want bool
	}{
		{name: "not_listed for two cycles spanning the minimum", obs: row(StateNotListed, 2, ConfirmationMinSpan), want: true},
		{name: "archived for two cycles spanning the minimum", obs: row(StateArchivedExcluded, 2, ConfirmationMinSpan), want: true},
		{name: "rule excluded for two cycles spanning the minimum", obs: row(StateRuleExcluded, 2, time.Hour), want: true},
		{name: "first evaluation confirms nothing", obs: row(StateNotListed, 1, 0), want: false},
		{name: "first evaluation of an archived repository confirms nothing", obs: row(StateArchivedExcluded, 1, 0), want: false},
		{name: "one cycle is pending however long ago", obs: row(StateRuleExcluded, 1, time.Hour), want: false},
		{name: "two cycles inside the minimum span are pending", obs: row(StateNotListed, 2, ConfirmationMinSpan-time.Second), want: false},
		{name: "selected is never confirmed", obs: row(StateSelected, 9, time.Hour), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Confirmed(tt.obs); got != tt.want {
				t.Fatalf("Confirmed() = %v, want %v", got, tt.want)
			}
		})
	}
	if ConfirmationMinSpan != 5*time.Minute {
		t.Fatalf("ConfirmationMinSpan = %v, want 5m", ConfirmationMinSpan)
	}
}
