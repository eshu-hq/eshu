// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"testing"
	"time"
)

// TestDerivedTargetRotationBucketSharesThePlanKeyBasis proves the slot bucket
// flips at exactly the moment the plan key does, including for an interval
// that does not divide the zero-time-to-epoch offset (#6720, #9422).
func TestDerivedTargetRotationBucketSharesThePlanKeyBasis(t *testing.T) {
	t.Parallel()

	for _, interval := range []time.Duration{30 * time.Minute, 5 * time.Hour, 7 * time.Minute, 0} {
		start := time.Date(2026, time.October, 28, 16, 0, 0, 0, time.UTC)
		previousKey := DerivedTargetPlanKey("continuous", start, interval, PlanningModeRotating)
		previousBucket := DerivedTargetRotationBucket(start, interval)
		for step := 1; step <= 3000; step++ {
			at := start.Add(time.Duration(step) * 7 * time.Second)
			key := DerivedTargetPlanKey("continuous", at, interval, PlanningModeRotating)
			bucket := DerivedTargetRotationBucket(at, interval)
			if (key != previousKey) != (bucket != previousBucket) {
				t.Fatalf("interval %s at %s: plan key changed %v but bucket changed %v",
					interval, at, key != previousKey, bucket != previousBucket)
			}
			if bucket != previousBucket && bucket != previousBucket+1 {
				t.Fatalf("interval %s at %s: bucket jumped %d -> %d, want consecutive", interval, at, previousBucket, bucket)
			}
			previousKey, previousBucket = key, bucket
		}
	}
}

func TestDerivedTargetRotationBucketForMode(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 28, 17, 0, 0, 0, time.UTC)
	interval := 30 * time.Minute
	rotating := DerivedTargetRotationBucketForMode(PlanningModeRotating, at, interval)
	if rotating <= 0 {
		t.Fatalf("rotating bucket = %d, want positive", rotating)
	}
	if got := DerivedTargetRotationBucketForMode("", at, interval); got != rotating {
		t.Fatalf("blank mode bucket = %d, want rotating %d", got, rotating)
	}
	if got := DerivedTargetRotationBucketForMode(PlanningModeSinglePass, at, interval); got != 0 {
		t.Fatalf("single_pass bucket = %d, want 0", got)
	}
	if got := DerivedTargetRotationBucket(time.Time{}, interval); got != 0 {
		t.Fatalf("zero time bucket = %d, want 0", got)
	}
	if got := DerivedTargetRotationBucket(time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC), interval); got != 0 {
		t.Fatalf("pre-epoch bucket = %d, want clamped 0", got)
	}
	// The offset the installed-advisory readers use stays bucket * limit.
	if got, want := DerivedTargetRotationOffset(at, interval, 100), rotating*100; got != want {
		t.Fatalf("rotation offset = %d, want bucket*limit %d", got, want)
	}
}

// TestDerivedTargetWindowStartUnixIsIntervalInvariant pins the #9776 fix: the
// window start is a time, so a window opened under a 30 minute interval and one
// opened under a 1 hour interval order by when they started, where the
// interval-relative bucket index would let the wider interval look decades older.
func TestDerivedTargetWindowStartUnixIsIntervalInvariant(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 2, 10, 45, 30, 0, time.UTC)
	if got, want := DerivedTargetWindowStartUnix(at, 30*time.Minute), time.Date(2026, time.November, 2, 10, 30, 0, 0, time.UTC).Unix(); got != want {
		t.Fatalf("30m window start = %d, want %d", got, want)
	}
	if got, want := DerivedTargetWindowStartUnix(at, time.Hour), time.Date(2026, time.November, 2, 10, 0, 0, 0, time.UTC).Unix(); got != want {
		t.Fatalf("1h window start = %d, want %d", got, want)
	}
	before := DerivedTargetWindowStartUnix(at, 30*time.Minute)
	after := DerivedTargetWindowStartUnix(at.Add(time.Hour), time.Hour)
	if after <= before {
		t.Fatalf("1h window after a 30m window started at %d, want later than %d", after, before)
	}
	if got := DerivedTargetRotationBucket(at.Add(time.Hour), time.Hour); got >= DerivedTargetRotationBucket(at, 30*time.Minute) {
		t.Fatalf("bucket index %d did not shrink when the interval doubled; the premise of the defect changed", got)
	}
	if got := DerivedTargetWindowStartUnix(time.Time{}, time.Hour); got != 0 {
		t.Fatalf("zero clock window start = %d, want 0", got)
	}
	if got := DerivedTargetWindowStartUnix(time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC), time.Hour); got != 0 {
		t.Fatalf("pre-1970 window start = %d, want 0", got)
	}
}
