// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pacingLatchKey names one shared latch row: its closed signal and the
// domain column ("" for the global ceiling, "kind/instance" for a depth
// family, the source system for a reducer source).
type pacingLatchKey struct {
	signal workflow.ClaimPacingSignal
	domain string
}

// pacingGaugeHold records that this process raised the active gauge for one
// latch row: the engaged_at it wrote (the engagement's identity) and the
// local instant the raise was recorded. Only the process that moved the row
// to engaged holds the +1, so only that process ever takes it back, and no
// process can read below zero.
type pacingGaugeHold struct {
	engagedAt time.Time
	raisedAt  time.Time
}

// writeClaimPacingLatch moves one latch row to the desired state and
// accounts the transition exactly once, in the process whose statement made
// it.
//
// The statement writes only when the stored engaged bit differs from the
// desired one (claimPacingLatchUpsert), so under READ COMMITTED a racing
// consult that read the same prior state either moves the row or matches
// nothing: PostgreSQL locks the conflicting row, waits out a concurrent
// writer, and re-evaluates the DO UPDATE ... WHERE against the latest
// committed version. Zero rows changed means another consult already made
// this transition and owns its telemetry; engaged_at stays the first
// writer's, and updated_at is the time of the last real transition.
func (s *WorkflowControlStore) writeClaimPacingLatch(
	ctx context.Context,
	key pacingLatchKey,
	engaged bool,
	now time.Time,
) error {
	stamp := now.UTC().Truncate(time.Microsecond)
	var engagedAt any
	if engaged {
		engagedAt = stamp
	}
	result, err := s.database.ExecContext(ctx, claimPacingLatchUpsert,
		string(key.signal), key.domain, engaged, engagedAt, stamp)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim pacing latch rows affected: %w", err)
	}
	if changed == 0 {
		return nil
	}
	s.recordClaimPacingTransition(ctx, key, engaged, stamp)
	return nil
}

// recordClaimPacingTransition emits the operator signals for one latch
// transition this process made: an engagement adds one to the engaged
// counter and raises the active gauge, a release takes back the raise this
// process holds. A release by a process that never raised the gauge changes
// nothing here; the process that did raise it drops it when it next
// observes the row (observeClaimPacingLatch). Nil instruments are a no-op,
// so binaries without a wired meter provider are unaffected.
func (s *WorkflowControlStore) recordClaimPacingTransition(
	ctx context.Context,
	key pacingLatchKey,
	engaged bool,
	engagedAt time.Time,
) {
	if s.Instruments == nil {
		return
	}
	attrs := claimPacingMetricAttrs(key.signal, key.domain)
	if !engaged {
		if s.dropPacingGaugeHold(key) && s.Instruments.WorkflowClaimPacingActive != nil {
			s.Instruments.WorkflowClaimPacingActive.Add(ctx, -1, attrs)
		}
		return
	}
	if s.Instruments.WorkflowClaimPacingEngagedTotal != nil {
		s.Instruments.WorkflowClaimPacingEngagedTotal.Add(ctx, 1, attrs)
	}
	s.pacingMu.Lock()
	_, held := s.pacingGaugeHolds[key]
	if s.pacingGaugeHolds == nil {
		s.pacingGaugeHolds = make(map[pacingLatchKey]pacingGaugeHold)
	}
	s.pacingGaugeHolds[key] = pacingGaugeHold{engagedAt: engagedAt, raisedAt: time.Now()}
	s.pacingMu.Unlock()
	// A hold left over from an earlier engagement this process never saw end
	// already carries its +1: the new engagement replaces it, not adds to it.
	if !held && s.Instruments.WorkflowClaimPacingActive != nil {
		s.Instruments.WorkflowClaimPacingActive.Add(ctx, 1, attrs)
	}
}

// dropPacingGaugeHold clears this process's hold on a latch row and reports
// whether there was one to take back.
func (s *WorkflowControlStore) dropPacingGaugeHold(key pacingLatchKey) bool {
	s.pacingMu.Lock()
	defer s.pacingMu.Unlock()
	if _, held := s.pacingGaugeHolds[key]; !held {
		return false
	}
	delete(s.pacingGaugeHolds, key)
	return true
}

// observeClaimPacingLatch settles this process's gauge against a latch row
// it just read. A process that holds the gauge for an engagement the row no
// longer shows (released by another process, or replaced by a newer
// engagement) takes its raise back, so the active gauge converges to the
// row from every process, not only the one that wrote the release. readAt
// is the local instant the read began: a hold recorded after it is newer
// than the read and is left alone.
func (s *WorkflowControlStore) observeClaimPacingLatch(
	ctx context.Context,
	key pacingLatchKey,
	row claimPacingLatchRow,
	readAt time.Time,
) {
	if s.Instruments == nil {
		return
	}
	s.pacingMu.Lock()
	hold, held := s.pacingGaugeHolds[key]
	stale := held && hold.raisedAt.Before(readAt) &&
		(!row.engaged || !row.engagedAt.Equal(hold.engagedAt))
	if stale {
		delete(s.pacingGaugeHolds, key)
	}
	s.pacingMu.Unlock()
	if stale && s.Instruments.WorkflowClaimPacingActive != nil {
		s.Instruments.WorkflowClaimPacingActive.Add(ctx, -1, claimPacingMetricAttrs(key.signal, key.domain))
	}
}

// observeSourcePacingLatches settles this process's gauge for every
// reducer-source row it holds against a full read of the source latch rows;
// a held source with no row reads as disengaged.
func (s *WorkflowControlStore) observeSourcePacingLatches(
	ctx context.Context,
	latches map[string]sourceLatchState,
	readAt time.Time,
) {
	if s.Instruments == nil {
		return
	}
	s.pacingMu.Lock()
	held := make([]pacingLatchKey, 0, len(s.pacingGaugeHolds))
	for key := range s.pacingGaugeHolds {
		if key.signal == workflow.ClaimPacingSignalReducerSource {
			held = append(held, key)
		}
	}
	s.pacingMu.Unlock()
	for _, key := range held {
		latch := latches[key.domain]
		s.observeClaimPacingLatch(ctx, key, claimPacingLatchRow(latch), readAt)
	}
}
