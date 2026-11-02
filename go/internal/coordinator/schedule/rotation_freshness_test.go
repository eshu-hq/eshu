// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"math"
	"testing"
	"time"
)

func TestOwnedTargetRotationFreshness(t *testing.T) {
	t.Parallel()

	const halfHour = 30 * time.Minute
	tests := []struct {
		name         string
		slotCount    int64
		interval     time.Duration
		maxStaleness time.Duration
		wantPeriod   time.Duration
		wantBound    time.Duration
		wantExceeds  bool
	}{
		{
			// QA today: P 1,024 at a 30-minute interval is the 21.3-day cycle
			// nobody was told about (#9775).
			name: "default limit at QA scale exceeds the default bound", slotCount: 1024, interval: halfHour,
			maxStaleness: 0, wantPeriod: 512 * time.Hour, wantBound: DefaultOwnedTargetMaxStaleness, wantExceeds: true,
		},
		{
			name: "P 32 at 30 minutes is a 16 hour rotation inside 24 hours", slotCount: 32, interval: halfHour,
			maxStaleness: 24 * time.Hour, wantPeriod: 16 * time.Hour, wantBound: 24 * time.Hour, wantExceeds: false,
		},
		{
			name: "period equal to the bound is within it", slotCount: 48, interval: halfHour,
			maxStaleness: 24 * time.Hour, wantPeriod: 24 * time.Hour, wantBound: 24 * time.Hour, wantExceeds: false,
		},
		{
			name: "one nanosecond over the bound exceeds it", slotCount: 48, interval: halfHour,
			maxStaleness: 24*time.Hour - time.Nanosecond, wantPeriod: 24 * time.Hour, wantBound: 24*time.Hour - time.Nanosecond, wantExceeds: true,
		},
		{
			name: "blank interval uses the reconcile default like the bucket does", slotCount: 4, interval: 0,
			maxStaleness: time.Hour, wantPeriod: 4 * DefaultReconcileInterval, wantBound: time.Hour, wantExceeds: false,
		},
		{
			name: "negative bound falls back to the default", slotCount: 1, interval: halfHour,
			maxStaleness: -time.Hour, wantPeriod: halfHour, wantBound: DefaultOwnedTargetMaxStaleness, wantExceeds: false,
		},
		{
			name: "overflowing product saturates instead of wrapping negative", slotCount: 1 << 40, interval: 48 * time.Hour,
			maxStaleness: 24 * time.Hour, wantPeriod: time.Duration(math.MaxInt64), wantBound: 24 * time.Hour, wantExceeds: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OwnedTargetRotationFreshness(tt.slotCount, tt.interval, tt.maxStaleness)
			if got.Period != tt.wantPeriod {
				t.Errorf("Period = %v, want %v", got.Period, tt.wantPeriod)
			}
			if got.MaxStaleness != tt.wantBound {
				t.Errorf("MaxStaleness = %v, want %v", got.MaxStaleness, tt.wantBound)
			}
			if got.Exceeds() != tt.wantExceeds {
				t.Errorf("Exceeds() = %v, want %v", got.Exceeds(), tt.wantExceeds)
			}
		})
	}
}

func TestOwnedTargetRotationFreshnessUnknownForNoSlots(t *testing.T) {
	t.Parallel()

	for _, slotCount := range []int64{0, -1} {
		got := OwnedTargetRotationFreshness(slotCount, 30*time.Minute, time.Hour)
		if got.Known() {
			t.Errorf("slotCount %d: Known() = true, want false (no read happened)", slotCount)
		}
		if got.Exceeds() {
			t.Errorf("slotCount %d: Exceeds() = true, want false for an unknown period", slotCount)
		}
	}
}
