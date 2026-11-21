// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"
)

// WinnersAuditSweep is the persisted sweep row (migration 335).
type WinnersAuditSweep struct {
	// SweepID numbers the sweep in progress; it grows by one at each sweep end.
	SweepID int64
	// Cursor is the last identifier of the last completed range; nil at the
	// start of a sweep.
	Cursor *WinnersAuditRangeIdent
	// RangesDone counts the ranges this sweep has committed.
	RangesDone int64
	// StartedAt is when the sweep's first range committed; zero at the start.
	StartedAt time.Time
	// LastCompletedAt is when the last completed sweep ended; zero if none.
	LastCompletedAt time.Time
	// LastSeconds, LastRanges, LastFacts, LastCandidates, LastRepairs and
	// LastDiscards describe the last completed sweep.
	LastSeconds    float64
	LastRanges     int64
	LastFacts      int64
	LastCandidates int64
	LastRepairs    int64
	LastDiscards   int64
}

// ReadWinnersAuditSweep reads the sweep row on the writer, creating it on
// first use.
func (s SupplyChainImpactWinnersStore) ReadWinnersAuditSweep(ctx context.Context) (WinnersAuditSweep, error) {
	// RED stub (#11131): no persisted cursor yet.
	_ = ctx
	return WinnersAuditSweep{SweepID: 1}, nil
}

// WinnersAuditRangeVerify is one range handed to the writer.
type WinnersAuditRangeVerify struct {
	// SweepID is the sweep the range was read in.
	SweepID int64
	// Range is the standby read.
	Range WinnersAuditRange
	// WaitForLock takes the resweep lock with the waiting claim bounded by
	// WinnersAuditRangeLockTimeout instead of the try-lock.
	WaitForLock bool
	// Complete records the sweep as completed. It is valid only on the last
	// range, and only when the NULL probe read clean at both ends.
	Complete bool
}

// WinnersAuditRangeOutcome is what the range transaction did.
type WinnersAuditRangeOutcome struct {
	// Advanced reports that the cursor moved and the transaction committed.
	Advanced bool
	// Skipped reports a lost try-lock; nothing committed.
	Skipped bool
	// LockBusy reports that the waiting claim hit its lock_timeout; nothing
	// committed.
	LockBusy bool
	// CursorRace reports that another instance advanced this range first;
	// the transaction, repairs included, rolled back.
	CursorRace bool
	// Locked reports that the verify took the resweep lock.
	Locked bool
	// Candidates, Pending and Repairs count the range's differing
	// identifiers, those already queued, and the stale ones this range
	// queued. Discards are candidates that were not repaired.
	Candidates, Pending, Repairs, Discards int64
	// Completed reports that this range completed the sweep.
	Completed bool
	// Sweep is the cursor row after the commit: its SweepID, RangesDone and
	// last completion.
	Sweep WinnersAuditSweep
}

// VerifyWinnersAuditRange runs one range's writer transaction: seed the
// candidates, drop those already queued (no lock), and only when a
// candidate remains take the resweep lock (try-lock, or the waiting claim),
// verify it with the keyed statements, queue the stale identifiers nothing
// has queued since, and compare-and-set the cursor. A lost claim or a lost
// compare-and-set rolls everything back.
func (s SupplyChainImpactWinnersStore) VerifyWinnersAuditRange(ctx context.Context, v WinnersAuditRangeVerify) (WinnersAuditRangeOutcome, error) {
	// RED stub (#11131): no verify, no compare-and-set yet.
	_, _ = ctx, v
	return WinnersAuditRangeOutcome{}, nil
}
