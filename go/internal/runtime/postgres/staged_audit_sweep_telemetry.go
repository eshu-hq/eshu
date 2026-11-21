// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// StagedAuditRangeObservation is one committed range of the staged audit
// sweep (#11131).
type StagedAuditRangeObservation struct {
	// Facts is the fact rows the range recomputed.
	Facts int64
	// Candidates, Pending, Repairs and Discards count the range's differing
	// identifiers, those already queued, those it queued, and those it did
	// not repair.
	Candidates, Pending, Repairs, Discards int64
	// Duration is the range's wall time, standby read through writer commit.
	Duration time.Duration
	// Oversize reports a one-identifier range larger than the budget.
	Oversize bool
	// Budget is the row budget the read ran with after any timeout halving.
	Budget int
	// Locked reports a verify that took the resweep lock.
	Locked bool
}

// StagedAuditSweepState is the persisted sweep row as last read or written.
// It survives a roll because it comes from the database, not the process.
type StagedAuditSweepState struct {
	SweepID         int64
	RangesDone      int64
	LastCompletedAt time.Time
	LastSeconds     float64
	LastRanges      int64
	LastFacts       int64
	LastCandidates  int64
	LastRepairs     int64
	LastDiscards    int64
}

// stagedAuditSweepCounters is the sweep half of StagedAuditTelemetry; the
// recorder's mutex guards it.
type stagedAuditSweepCounters struct {
	ranges, retries                     int64
	locked                              int64
	facts, candidates, pending, repairs int64
	discards                            int64
	lastRange                           time.Duration
	lastRangeFacts                      int64
	lastRangeBudget                     int64
	state                               StagedAuditSweepState
	stateSeen                           bool
}

// ObserveRange counts one committed range.
func (t *StagedAuditTelemetry) ObserveRange(o StagedAuditRangeObservation) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := &t.sweep
	s.ranges++
	s.facts += o.Facts
	s.candidates += o.Candidates
	s.pending += o.Pending
	s.repairs += o.Repairs
	s.discards += o.Discards
	s.lastRange = o.Duration
	s.lastRangeFacts = o.Facts
	s.lastRangeBudget = int64(o.Budget)
	if o.Locked {
		s.locked++
	}
}

// LockedRanges reports committed ranges whose verify took the resweep lock.
func (t *StagedAuditTelemetry) LockedRanges() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sweep.locked
}

// ObserveRangeRetry counts one range read retried at half budget after a
// statement timeout.
func (t *StagedAuditTelemetry) ObserveRangeRetry() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep.retries++
}

// ObserveSweepState records the persisted sweep row: the sweep position and
// the last completed sweep.
func (t *StagedAuditTelemetry) ObserveSweepState(s StagedAuditSweepState) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep.state = s
	t.sweep.stateSeen = true
}

// Ranges reports committed ranges since process start.
func (t *StagedAuditTelemetry) Ranges() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sweep.ranges
}

// RangeRetries reports range reads retried at half budget.
func (t *StagedAuditTelemetry) RangeRetries() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sweep.retries
}

// SweepState reports the last persisted sweep row seen and whether one was.
func (t *StagedAuditTelemetry) SweepState() (StagedAuditSweepState, bool) {
	if t == nil {
		return StagedAuditSweepState{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sweep.state, t.sweep.stateSeen
}

// fill copies the sweep counters into the exported snapshot.
func (s stagedAuditSweepCounters) fill(snap *telemetry.SupplyChainImpactWinnersStagedAuditSnapshot) {
	snap.Ranges, snap.RangeRetries = s.ranges, s.retries
	snap.LockedRanges = s.locked
	snap.RangeFacts, snap.RangeCandidates, snap.RangePending = s.facts, s.candidates, s.pending
	snap.RangeRepairs, snap.RangeDiscards = s.repairs, s.discards
	snap.LastRange, snap.LastRangeFacts, snap.LastRangeBudget = s.lastRange, s.lastRangeFacts, s.lastRangeBudget
	if !s.stateSeen {
		return
	}
	snap.SweepSeen = true
	snap.SweepID, snap.SweepRangesDone = s.state.SweepID, s.state.RangesDone
	snap.LastSweepCompletedAt, snap.LastSweepSeconds = s.state.LastCompletedAt, s.state.LastSeconds
	snap.LastSweepRanges, snap.LastSweepFacts = s.state.LastRanges, s.state.LastFacts
	snap.LastSweepCandidates, snap.LastSweepRepairs = s.state.LastCandidates, s.state.LastRepairs
	snap.LastSweepDiscards = s.state.LastDiscards
}
