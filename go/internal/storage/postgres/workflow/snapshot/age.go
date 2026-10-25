// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"slices"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// AdvanceAges moves the snapshot's clock-relative durations from the row's
// as_of to the read, in place. age is the reader's database clock minus the
// row's as_of. The oldest-pending, oldest-retry, and oldest-claim ages grow by
// age; the next-retry delay shrinks by it and stops at zero. A zero duration
// means the live statement saw an empty set and clamped to zero, so it stays
// zero: adding age would invent an outstanding item. Counts, the recent-failure
// window, and the terminal failures are true at as_of and are not adjusted;
// the stale-after bound is what limits how far they can drift. A nil snapshot
// or a non-positive age changes nothing.
func AdvanceAges(snap *statuspkg.CoordinatorSnapshot, age time.Duration) {
	if snap == nil || age <= 0 {
		return
	}
	snap.OldestPendingAge = grow(snap.OldestPendingAge, age)
	for i := range snap.CollectorBackpressure {
		row := &snap.CollectorBackpressure[i]
		row.OldestPendingAge = grow(row.OldestPendingAge, age)
		row.OldestRetryAge = grow(row.OldestRetryAge, age)
		row.OldestClaimAge = grow(row.OldestClaimAge, age)
		if row.NextRetryDelay > 0 {
			row.NextRetryDelay = max(row.NextRetryDelay-age, 0)
		}
	}
}

func grow(value, age time.Duration) time.Duration {
	if value <= 0 {
		return value
	}
	return value + age
}

// Clone returns a deep copy of snap that shares no slice or pointer with it,
// so a snapshot shared among concurrent readers can be aged by each without
// racing. A nil snapshot clones to nil.
func Clone(snap *statuspkg.CoordinatorSnapshot) *statuspkg.CoordinatorSnapshot {
	if snap == nil {
		return nil
	}
	cloned := *snap
	cloned.CollectorInstances = slices.Clone(snap.CollectorInstances)
	cloned.RunStatusCounts = slices.Clone(snap.RunStatusCounts)
	cloned.WorkItemStatusCounts = slices.Clone(snap.WorkItemStatusCounts)
	cloned.CompletenessCounts = slices.Clone(snap.CompletenessCounts)
	cloned.CollectorTerminalFailures = slices.Clone(snap.CollectorTerminalFailures)
	if snap.CollectorBackpressure != nil {
		cloned.CollectorBackpressure = make([]collector.BackpressureSnapshot, len(snap.CollectorBackpressure))
		for i, row := range snap.CollectorBackpressure {
			row.FailureClassCounts = slices.Clone(row.FailureClassCounts)
			cloned.CollectorBackpressure[i] = row
		}
	}
	if snap.RecentFailures != nil {
		recent := *snap.RecentFailures
		cloned.RecentFailures = &recent
	}
	return &cloned
}
