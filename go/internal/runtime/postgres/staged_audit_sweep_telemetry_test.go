// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestStagedAuditSweepTelemetrySnapshot pins the #11131 recorder: ranges
// accumulate, the persisted sweep row is carried as last seen, and every new
// deferral reason is counted under its own name, not folded into batch_abort.
func TestStagedAuditSweepTelemetrySnapshot(t *testing.T) {
	t.Parallel()
	tel := NewStagedAuditTelemetry()
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 100, Candidates: 4, Pending: 1, Repairs: 2, Discards: 2, Duration: time.Second, Budget: 50})
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 30, Candidates: 1, Repairs: 1, Duration: 2 * time.Second, Budget: 25})
	tel.ObserveRangeRetry()
	done := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	tel.ObserveSweepState(StagedAuditSweepState{SweepID: 3, RangesDone: 9, LastCompletedAt: done, LastRanges: 40})
	for _, r := range []StagedAuditDeferralReason{StagedAuditDeferTimeout, StagedAuditDeferNullKey, StagedAuditDeferPipe, StagedAuditDeferLockBusy, StagedAuditDeferCursorRace} {
		tel.ObserveDeferral(r)
	}
	snap := tel.StagedAuditSnapshot()
	if snap.Ranges != 2 || snap.RangeFacts != 130 || snap.RangeRepairs != 3 || snap.RangePending != 1 || snap.RangeRetries != 1 {
		t.Fatalf("range counters = %+v", snap)
	}
	if snap.LastRange != 2*time.Second || snap.LastRangeBudget != 25 {
		t.Fatalf("last range = %v / %d, want the second range", snap.LastRange, snap.LastRangeBudget)
	}
	if !snap.SweepSeen || snap.SweepID != 3 || snap.SweepRangesDone != 9 || !snap.LastSweepCompletedAt.Equal(done) || snap.LastSweepRanges != 40 {
		t.Fatalf("sweep state = %+v", snap)
	}
	for _, r := range []string{"timeout", "null_key", "pipe", "lock_busy", "cursor_race"} {
		if snap.Deferrals[r] != 1 {
			t.Errorf("deferral %s = %d, want 1", r, snap.Deferrals[r])
		}
	}
	if snap.Deferrals["batch_abort"] != 0 {
		t.Fatalf("batch_abort = %d: a new reason was folded into it", snap.Deferrals["batch_abort"])
	}
}
