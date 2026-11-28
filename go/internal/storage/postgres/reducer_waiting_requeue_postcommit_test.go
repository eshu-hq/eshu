// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #11624: the waiting-wake requeue counter
// (eshu_dp_reducer_requeue_total{reason="waiting_wake"}) must count only
// transactions that committed. The sibling waiting-wake outcome counter
// (eshu_dp_reducer_waiting_wake_total) stays emitted at statement time, so a
// rolled-back caller transaction leaves the requeue counter at 0 while the
// outcome counter still records the statement.

// counterByAttr sums one int64 counter's data points whose attribute key
// equals value.
func counterByAttr(t *testing.T, reader *sdkmetric.ManualReader, name, key, value string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				got, _ := point.Attributes.Value(attribute.Key(key))
				if got.AsString() == value {
					total += point.Value
				}
			}
		}
	}
	return total
}

func waitingWakeRequeues(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	return counterByAttr(t, reader, "eshu_dp_reducer_requeue_total",
		telemetry.MetricDimensionReason, string(telemetry.RequeueReasonWaitingWake))
}

func waitingWakeOutcome(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	return counterByAttr(t, reader, "eshu_dp_reducer_waiting_wake_total",
		telemetry.MetricDimensionOutcome, outcome)
}

// requeueWakeFake answers every waiting-table wake statement with a scripted
// reopened count. It doubles as the transaction beginner so a store under
// test owns its own transaction.
type requeueWakeFake struct {
	reopened  int64
	commitErr error
}

func (f *requeueWakeFake) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResultWithRowsAffected{rowsAffected: 1}, nil
}

func (f *requeueWakeFake) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == terminalGenerationActiveWaitKeysQuery {
		return &recordingRows{}, nil
	}
	if strings.Contains(query, "reducer_waiting_items") {
		return &waitingWakeCountRows{reopened: f.reopened, cleared: f.reopened}, nil
	}
	return newRelationshipRows(nil), nil
}

func (f *requeueWakeFake) Begin(context.Context) (db.Transaction, error) {
	return requeueWakeFakeTx{f}, nil
}

type requeueWakeFakeTx struct{ *requeueWakeFake }

func (t requeueWakeFakeTx) Commit() error   { return t.commitErr }
func (t requeueWakeFakeTx) Rollback() error { return nil }

// TestActivationWakeRequeueCounterEmitsOnlyAfterCommit pins the relationship
// activation site: the wake runs inside the activation transaction, so a
// failed commit must leave the requeue counter at 0 while the outcome counter
// keeps the statement. A committed activation counts the reopened rows.
func TestActivationWakeRequeueCounterEmitsOnlyAfterCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		commitErr   error
		wantRequeue int64
	}{
		{name: "committed counts reopened rows", wantRequeue: 3},
		{name: "rolled back counts nothing", commitErr: errors.New("commit failed"), wantRequeue: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := newWakeMetrics(t)
			fake := &requeueWakeFake{reopened: 3, commitErr: tt.commitErr}
			err := NewRelationshipStore(fake).WithInstruments(instruments).
				ActivateResolutionGeneration(context.Background(), "gen-1", "scope-1")
			if (err != nil) != (tt.commitErr != nil) {
				t.Fatalf("activation error = %v, want error %v", err, tt.commitErr != nil)
			}
			if got := waitingWakeRequeues(t, reader); got != tt.wantRequeue {
				t.Fatalf("waiting_wake requeue = %d, want %d", got, tt.wantRequeue)
			}
			if got := waitingWakeOutcome(t, reader, telemetry.WaitingWakeOutcomeReopened); got < 1 {
				t.Fatalf("waiting-wake outcome reopened = %d, want the statement counted even on rollback", got)
			}
		})
	}
}

// TestPhaseStoreCallerOwnedTxRequeueCounterEmitsAfterCommitHook pins the
// caller-owned transaction site: a store built over a transaction defers the
// requeue counter until the owner reports the commit through AfterCommit,
// and an owner that rolled back (never calls AfterCommit) counts nothing.
func TestPhaseStoreCallerOwnedTxRequeueCounterEmitsAfterCommitHook(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	rows := []gpphase.PhaseState{{
		Key: gpphase.PhaseKey{
			ScopeID: "scope-1", AcceptanceUnitID: "scope-1", SourceRunID: "gen-1",
			GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCrossRepoEvidence,
		},
		Phase: gpphase.PhaseBackwardEvidenceCommitted, CommittedAt: now, UpdatedAt: now,
	}}

	t.Run("deferred until AfterCommit", func(t *testing.T) {
		t.Parallel()
		instruments, reader := newWakeMetrics(t)
		store := NewGraphProjectionPhaseStateStore(&requeueWakeFake{reopened: 2})
		store.Instruments = instruments
		store.DeferWakeRequeues()
		if err := store.Upsert(context.Background(), rows); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got := waitingWakeRequeues(t, reader); got != 0 {
			t.Fatalf("requeue before commit = %d, want 0", got)
		}
		store.AfterCommit(context.Background())
		after := waitingWakeRequeues(t, reader)
		if after < 2 {
			t.Fatalf("requeue after commit = %d, want >= 2 reopened rows", after)
		}
		store.AfterCommit(context.Background())
		if got := waitingWakeRequeues(t, reader); got != after {
			t.Fatalf("second AfterCommit re-emitted: %d -> %d", after, got)
		}
	})

	t.Run("rolled back owner counts nothing", func(t *testing.T) {
		t.Parallel()
		instruments, reader := newWakeMetrics(t)
		// The owner's transaction fails to commit: the upsert ran inside it,
		// the owner rolls back, and AfterCommit is reached only on a nil
		// Commit result, exactly as the backfill fan-in does (its production
		// path is pinned by TestBackfillEngineRequeueCounterFollowsCommit).
		fake := &requeueWakeFake{reopened: 2, commitErr: errors.New("commit failed")}
		tx, err := fake.Begin(context.Background())
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		store := NewGraphProjectionPhaseStateStore(tx)
		store.Instruments = instruments
		store.DeferWakeRequeues()
		if err := store.Upsert(context.Background(), rows); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := tx.Commit(); err == nil {
			store.AfterCommit(context.Background())
			t.Fatal("Commit error = nil, want the scripted commit failure")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if got := waitingWakeRequeues(t, reader); got != 0 {
			t.Fatalf("requeue after rollback = %d, want 0", got)
		}
		if got := waitingWakeOutcome(t, reader, telemetry.WaitingWakeOutcomeReopened); got < 1 {
			t.Fatalf("outcome reopened = %d, want the statement counted", got)
		}
	})

	t.Run("autocommit store emits at once", func(t *testing.T) {
		t.Parallel()
		instruments, reader := newWakeMetrics(t)
		store := NewGraphProjectionPhaseStateStore(&requeueWakeFake{reopened: 2})
		store.Instruments = instruments
		if err := store.Upsert(context.Background(), rows); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got := waitingWakeRequeues(t, reader); got < 2 {
			t.Fatalf("autocommit requeue = %d, want >= 2 emitted at statement time", got)
		}
	})
}

// TestWinnersWakeRequeueCounterFollowsDeferredCommit pins the winners
// watermark wake: under a deferral the count waits for the commit flush.
func TestWinnersWakeRequeueCounterFollowsDeferredCommit(t *testing.T) {
	t.Parallel()
	instruments, reader := newWakeMetrics(t)
	fake := &requeueWakeFake{reopened: 4}
	ctx, deferred := deferWakeRequeues(context.Background())
	reopened, err := wakeWinnersWatermark(ctx, fake, instruments, WaitingWakeSiteInTx,
		time.Now().UTC(), time.Now().UTC())
	if err != nil || reopened != 4 {
		t.Fatalf("wakeWinnersWatermark = (%d, %v), want (4, nil)", reopened, err)
	}
	if got := waitingWakeRequeues(t, reader); got != 0 {
		t.Fatalf("requeue before commit = %d, want 0", got)
	}
	deferred.flush(context.Background(), instruments)
	if got := waitingWakeRequeues(t, reader); got != 4 {
		t.Fatalf("requeue after flush = %d, want 4", got)
	}
}

// TestProjectorAckWakeRequeueCounterEmitsOnlyAfterCommit pins the projector
// Ack site: its in-tx wakes defer, and a failed commit counts nothing.
func TestProjectorAckWakeRequeueCounterEmitsOnlyAfterCommit(t *testing.T) {
	t.Parallel()
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-1"},
	}
	tests := []struct {
		name      string
		commitErr error
		want      int64
	}{
		{name: "committed counts reopened rows", want: 1},
		{name: "rolled back counts nothing", commitErr: errors.New("commit failed"), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := newWakeMetrics(t)
			fake := &requeueWakeFake{reopened: 1, commitErr: tt.commitErr}
			queue := NewProjectorQueue(fake, "projector-1", 30*time.Second)
			queue.Instruments = instruments
			queue.skipAckPostCommitWake = true
			queue.Now = func() time.Time { return time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC) }
			err := queue.Ack(context.Background(), work, runtime.Result{})
			if (err != nil) != (tt.commitErr != nil) {
				t.Fatalf("Ack error = %v, want error %v", err, tt.commitErr != nil)
			}
			if got := waitingWakeRequeues(t, reader); got != tt.want {
				t.Fatalf("waiting_wake requeue = %d, want %d", got, tt.want)
			}
		})
	}
}
