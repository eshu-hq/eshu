// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestStagedAuditSweepAccessorsReadWhatWasObserved pins the sweep half of the
// recorder (#11131) end to end: committed ranges sum, only locked ranges count
// as locked, retries count, the persisted sweep row is returned whole, and the
// exported snapshot carries every one of those figures.
func TestStagedAuditSweepAccessorsReadWhatWasObserved(t *testing.T) {
	t.Parallel()
	tel := NewStagedAuditTelemetry()
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 100, Candidates: 4, Pending: 1, Repairs: 2, Discards: 1, Locked: true, Budget: 50})
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 20, Candidates: 2, Pending: 1, Repairs: 1, Discards: 0, Locked: true, Budget: 50})
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 30, Candidates: 3, Pending: 2, Repairs: 1, Discards: 2, Budget: 25})
	tel.ObserveRangeRetry()
	tel.ObserveRangeRetry()
	done := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	want := StagedAuditSweepState{
		SweepID: 7, RangesDone: 11, LastCompletedAt: done, LastSeconds: 12.5,
		LastRanges: 40, LastFacts: 400, LastCandidates: 9, LastRepairs: 6, LastDiscards: 3,
	}
	tel.ObserveSweepState(want)

	if got := tel.Ranges(); got != 3 {
		t.Fatalf("Ranges = %d, want 3", got)
	}
	if got := tel.LockedRanges(); got != 2 {
		t.Fatalf("LockedRanges = %d, want 2 of 3: an unlocked range must not count", got)
	}
	if got := tel.RangeRetries(); got != 2 {
		t.Fatalf("RangeRetries = %d, want 2", got)
	}
	got, seen := tel.SweepState()
	if !seen || got != want {
		t.Fatalf("SweepState = %+v, %v; want %+v, true", got, seen, want)
	}

	snap := tel.StagedAuditSnapshot()
	if snap.LockedRanges != 2 || snap.RangeCandidates != 9 || snap.RangePending != 4 || snap.RangeDiscards != 3 || snap.LastRangeFacts != 30 {
		t.Fatalf("range figures in the snapshot = %+v", snap)
	}
	if !snap.SweepSeen || snap.SweepID != 7 || snap.SweepRangesDone != 11 || snap.LastSweepSeconds != 12.5 ||
		snap.LastSweepRanges != 40 || snap.LastSweepFacts != 400 || snap.LastSweepCandidates != 9 ||
		snap.LastSweepRepairs != 6 || snap.LastSweepDiscards != 3 || !snap.LastSweepCompletedAt.Equal(done) {
		t.Fatalf("sweep row in the snapshot = %+v", snap)
	}
}

// TestStagedAuditSweepStateIsAbsentUntilObserved pins that a recorder that has
// not read the sweep row reports none, so the exported gauges stay unset
// instead of publishing a zero sweep as if it were real.
func TestStagedAuditSweepStateIsAbsentUntilObserved(t *testing.T) {
	t.Parallel()
	tel := NewStagedAuditTelemetry()
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 5})
	if state, seen := tel.SweepState(); seen || state != (StagedAuditSweepState{}) {
		t.Fatalf("SweepState before any read = %+v, %v; want zero, false", state, seen)
	}
	if snap := tel.StagedAuditSnapshot(); snap.SweepSeen || snap.SweepID != 0 || snap.Ranges != 1 {
		t.Fatalf("snapshot = %+v; want ranges counted and no sweep row", snap)
	}
}

// TestStagedAuditSweepTelemetryIsSafeOnANilRecorder pins the documented nil
// contract the auditor relies on: with no recorder wired, every observer and
// reader is a no-op that reports zero.
func TestStagedAuditSweepTelemetryIsSafeOnANilRecorder(t *testing.T) {
	t.Parallel()
	var tel *StagedAuditTelemetry
	tel.ObserveRange(StagedAuditRangeObservation{Facts: 1, Locked: true})
	tel.ObserveRangeRetry()
	tel.ObserveSweepState(StagedAuditSweepState{SweepID: 1})
	tel.ObserveDeferral(StagedAuditDeferKeyedYield)
	if tel.Ranges() != 0 || tel.LockedRanges() != 0 || tel.RangeRetries() != 0 || tel.Deferrals(StagedAuditDeferKeyedYield) != 0 {
		t.Fatal("a nil recorder reported a non-zero count")
	}
	if state, seen := tel.SweepState(); seen || state != (StagedAuditSweepState{}) {
		t.Fatalf("nil SweepState = %+v, %v", state, seen)
	}
}

// TestStagedAuditDeferralReasonsAreClosed pins the closed set: keyed_yield is
// counted under its own name, and a reason outside the set folds into
// batch_abort instead of minting a new metric label.
func TestStagedAuditDeferralReasonsAreClosed(t *testing.T) {
	t.Parallel()
	tel := NewStagedAuditTelemetry()
	tel.ObserveDeferral(StagedAuditDeferKeyedYield)
	tel.ObserveDeferral(StagedAuditDeferKeyedYield)
	tel.ObserveDeferral(StagedAuditDeferralReason("made_up_reason"))
	if got := tel.Deferrals(StagedAuditDeferKeyedYield); got != 2 {
		t.Fatalf("keyed_yield = %d, want 2", got)
	}
	if got := tel.Deferrals(StagedAuditDeferBatchAbort); got != 1 {
		t.Fatalf("batch_abort = %d, want 1: an unknown reason folds into it", got)
	}
	if got := tel.Deferrals(StagedAuditDeferralReason("made_up_reason")); got != 0 {
		t.Fatalf("made_up_reason = %d: an unknown reason must not become its own label", got)
	}
	snap := tel.StagedAuditSnapshot()
	if snap.Deferrals["keyed_yield"] != 2 || snap.Deferrals["batch_abort"] != 1 {
		t.Fatalf("snapshot deferrals = %v", snap.Deferrals)
	}
}

// TestClassifyStagedAuditErrorReadsTheDeferralReason pins how a failure maps
// to its deferral reason, including a sweep reason carried by a typed error:
// the typed reason wins, and the wrapped cause stays reachable for logs.
func TestClassifyStagedAuditErrorReadsTheDeferralReason(t *testing.T) {
	t.Parallel()
	if reason, ok := ClassifyStagedAuditError(nil); ok || reason != "" {
		t.Fatalf("nil error = %q, %v; want none", reason, ok)
	}
	cause := errors.New("range read timed out")
	typed := &StagedAuditDeferredError{Reason: StagedAuditDeferTimeout, Err: cause}
	if reason, ok := ClassifyStagedAuditError(fmt.Errorf("tick: %w", typed)); !ok || reason != StagedAuditDeferTimeout {
		t.Fatalf("typed timeout = %q, %v; want timeout", reason, ok)
	}
	if !errors.Is(typed, cause) {
		t.Fatal("the typed deferral error must unwrap to its cause")
	}
	if got := typed.Error(); got != "staged audit tick deferred (timeout): range read timed out" {
		t.Fatalf("Error() = %q", got)
	}
	if reason, ok := ClassifyStagedAuditError(errors.New("connection reset")); !ok || reason != StagedAuditDeferBatchAbort {
		t.Fatalf("plain failure = %q, %v; want batch_abort", reason, ok)
	}
}
