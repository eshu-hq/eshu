// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"testing"
	"time"
)

// TestSupplyChainImpactHeadroomTrackerWindowEdgeIsExactlySixtyMinutes pins the
// window boundary: a pass stays visible through the 60th minute bucket it falls
// in and is gone in the next. The window is 60 one-minute buckets, so a pass is
// visible for 59 to 60 minutes depending on where it sits in its minute.
func TestSupplyChainImpactHeadroomTrackerWindowEdgeIsExactlySixtyMinutes(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, 11, 1, 12, 0, 30, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", nil, 50, 100, recordedAt)

	lastVisible := time.Date(2026, 11, 1, 12, 59, 59, 0, time.UTC)
	if ratio, ok := tracker.WidestBudgetRatio(lastVisible); !ok || ratio != 0.5 {
		t.Fatalf("WidestBudgetRatio(%s) = %v, %v; want 0.5, true: the pass is in the 60th bucket back", lastVisible, ratio, ok)
	}
	firstGone := time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)
	if ratio, ok := tracker.WidestBudgetRatio(firstGone); ok {
		t.Fatalf("WidestBudgetRatio(%s) = %v, true; want no sample: the pass is 61 buckets back", firstGone, ratio)
	}
}

// TestSupplyChainImpactHeadroomTrackerSkipsBucketsFromTheFuture pins that a
// wall clock that steps backwards cannot surface a bucket whose minute is after
// the read time: such a sample belongs to no minute the window covers.
func TestSupplyChainImpactHeadroomTrackerSkipsBucketsFromTheFuture(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, 11, 1, 12, 30, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", nil, 500, 100, recordedAt)

	stepBack := recordedAt.Add(-10 * time.Minute)
	if ratio, ok := tracker.WidestBudgetRatio(stepBack); ok {
		t.Fatalf("WidestBudgetRatio(%s) = %v, true; want no sample for a bucket 10 minutes in the future", stepBack, ratio)
	}
	if ratio, ok := tracker.WidestBudgetRatio(recordedAt); !ok || ratio != 5 {
		t.Fatalf("WidestBudgetRatio(%s) = %v, %v; want 5, true once the clock reaches the bucket", recordedAt, ratio, ok)
	}
}

// TestSupplyChainImpactHeadroomTrackerCountsAScopeOnceForRepeatedUnknownCauses
// pins that several unknown causes on one scope fold to one "other" entry, so
// one stuck scope is not counted twice under the same label.
func TestSupplyChainImpactHeadroomTrackerCountsAScopeOnceForRepeatedUnknownCauses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{"future_cause_one", "future_cause_two", SupplyChainImpactPartialCauseOther}, 1, 100, now)

	got := tracker.PartialByCause()
	if got[SupplyChainImpactPartialCauseOther] != 1 {
		t.Fatalf("PartialByCause() = %v, want other=1 for one scope with three unknown causes", got)
	}
}
