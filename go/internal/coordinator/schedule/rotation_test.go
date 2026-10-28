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
