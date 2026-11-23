// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// scriptedLatchDB answers every latch write with the scripted rows-affected
// count, so a proof can play the racing loser (0) and the winner (1) without
// a database.
type scriptedLatchDB struct {
	sourceBackpressureFakeDB
	affected int64
}

func (f *scriptedLatchDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if _, err := f.sourceBackpressureFakeDB.ExecContext(ctx, query, args...); err != nil {
		return nil, err
	}
	return affectedResult{affected: f.affected}, nil
}

var transitionTestKey = pacingLatchKey{signal: workflow.ClaimPacingSignalReducerSource, domain: "src-a"}

// TestWriteClaimPacingLatchLoserCountsNothing pins the #10882 accounting: a
// consult whose latch write changed no row lost the transition to another
// consult, so it records no engagement and raises no gauge.
func TestWriteClaimPacingLatchLoserCountsNothing(t *testing.T) {
	store, reader := newPacingSignalsStore(t)
	store.database = &scriptedLatchDB{affected: 0}

	if err := store.writeClaimPacingLatch(context.Background(), transitionTestKey, true, time.Now()); err != nil {
		t.Fatalf("writeClaimPacingLatch() error = %v, want nil", err)
	}
	for _, name := range []string{pacingSignalsEngagedMetric, pacingSignalsActiveMetric} {
		if points := pacingSignalPoints(t, reader, name); len(points) != 0 {
			t.Fatalf("%s = %+v, want no point from a losing consult", name, points)
		}
	}
	if len(store.pacingGaugeHolds) != 0 {
		t.Fatalf("pacingGaugeHolds = %+v, want none from a losing consult", store.pacingGaugeHolds)
	}
}

// TestWriteClaimPacingLatchWinnerCountsOnceAndHoldsGauge pins the winner
// side: one engagement, one gauge raise held by this process, and a second
// engagement over a hold this process never saw end does not stack a raise.
func TestWriteClaimPacingLatchWinnerCountsOnceAndHoldsGauge(t *testing.T) {
	store, reader := newPacingSignalsStore(t)
	store.database = &scriptedLatchDB{affected: 1}
	now := time.Now()

	if err := store.writeClaimPacingLatch(context.Background(), transitionTestKey, true, now); err != nil {
		t.Fatalf("writeClaimPacingLatch() error = %v, want nil", err)
	}
	if got := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric)["reducer_source"].value; got != 1 {
		t.Fatalf("engaged_total = %v, want 1", got)
	}
	if got := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)["reducer_source"].value; got != 1 {
		t.Fatalf("active = %v, want 1", got)
	}

	if err := store.writeClaimPacingLatch(context.Background(), transitionTestKey, true, now.Add(time.Minute)); err != nil {
		t.Fatalf("second writeClaimPacingLatch() error = %v, want nil", err)
	}
	if got := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric)["reducer_source"].value; got != 2 {
		t.Fatalf("engaged_total = %v, want 2 (two real transitions)", got)
	}
	if got := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)["reducer_source"].value; got != 1 {
		t.Fatalf("active = %v, want 1: a replaced hold must not stack a second raise", got)
	}
}

// TestObserveClaimPacingLatchSettlesHoldAgainstRow pins the observation
// rule: a process drops its raise when a read that began after the raise
// shows the row disengaged or on another engagement, keeps it for the same
// engagement, keeps it against a read that began before the raise, and never
// drops it twice.
func TestObserveClaimPacingLatchSettlesHoldAgainstRow(t *testing.T) {
	ctx := context.Background()
	engagedAt := time.Now().UTC().Truncate(time.Microsecond)
	raise := func(t *testing.T) (*WorkflowControlStore, func() float64) {
		store, reader := newPacingSignalsStore(t)
		store.database = &scriptedLatchDB{affected: 1}
		if err := store.writeClaimPacingLatch(ctx, transitionTestKey, true, engagedAt); err != nil {
			t.Fatalf("writeClaimPacingLatch() error = %v, want nil", err)
		}
		return store, func() float64 {
			return pacingSignalPoints(t, reader, pacingSignalsActiveMetric)["reducer_source"].value
		}
	}
	later := func() time.Time { return time.Now().Add(time.Second) }

	t.Run("same engagement keeps the raise", func(t *testing.T) {
		store, active := raise(t)
		store.observeClaimPacingLatch(ctx, transitionTestKey, claimPacingLatchRow{engaged: true, engagedAt: engagedAt}, later())
		if got := active(); got != 1 {
			t.Fatalf("active = %v, want 1", got)
		}
	})
	t.Run("disengaged row drops the raise once", func(t *testing.T) {
		store, active := raise(t)
		store.observeClaimPacingLatch(ctx, transitionTestKey, claimPacingLatchRow{}, later())
		store.observeClaimPacingLatch(ctx, transitionTestKey, claimPacingLatchRow{}, later())
		if got := active(); got != 0 {
			t.Fatalf("active = %v, want 0 (dropped once, never below zero)", got)
		}
	})
	t.Run("a newer engagement by another process drops the raise", func(t *testing.T) {
		store, active := raise(t)
		store.observeClaimPacingLatch(ctx, transitionTestKey,
			claimPacingLatchRow{engaged: true, engagedAt: engagedAt.Add(time.Hour)}, later())
		if got := active(); got != 0 {
			t.Fatalf("active = %v, want 0: the engagement this process raised for is over", got)
		}
	})
	t.Run("a read that began before the raise is stale", func(t *testing.T) {
		store, active := raise(t)
		store.observeClaimPacingLatch(ctx, transitionTestKey, claimPacingLatchRow{}, time.Now().Add(-time.Hour))
		if got := active(); got != 1 {
			t.Fatalf("active = %v, want 1: a read older than the raise proves nothing", got)
		}
	})
}

// TestSourcePacingStatesObservationDropsReleasedGauge pins that the status
// publication read settles the gauge too: a process that raised the gauge
// and then only publishes status still drops it once the source row reads
// disengaged.
func TestSourcePacingStatesObservationDropsReleasedGauge(t *testing.T) {
	store, reader := newPacingSignalsStore(t)
	store.database = &scriptedLatchDB{affected: 1}
	if err := store.writeClaimPacingLatch(context.Background(), transitionTestKey, true, time.Now()); err != nil {
		t.Fatalf("writeClaimPacingLatch() error = %v, want nil", err)
	}
	// The publication read sees no signals and no latch rows.
	time.Sleep(2 * time.Millisecond)
	if _, err := store.SourcePacingStates(context.Background(), sourceBackpressureTestConfig(), time.Now()); err != nil {
		t.Fatalf("SourcePacingStates() error = %v, want nil", err)
	}
	if got := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)["reducer_source"].value; got != 0 {
		t.Fatalf("active = %v after a disengaged read, want 0", got)
	}
}
