// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #11624 wiring proofs. The primitive tests in
// reducer_waiting_requeue_postcommit_test.go pin the deferral itself. These
// tests pin that each production transaction owner actually opens the
// deferral and flushes it only after a successful Commit: deleting the
// DeferWakeRequeues call from the backfill seam, or the deferWakeRequeues and
// flush pair from either winners path, turns one of them red.

// requeueEngineDB is a Beginner whose transactions also answer the backfill
// fan-in's active-generation re-read, so the real engine can run over the
// shared requeueWakeFake.
type requeueEngineDB struct {
	*requeueWakeFake
	generation string
}

func (d requeueEngineDB) Begin(context.Context) (db.Transaction, error) {
	return requeueEngineTx{requeueWakeFakeTx: requeueWakeFakeTx{d.requeueWakeFake}, generation: d.generation}, nil
}

type requeueEngineTx struct {
	requeueWakeFakeTx
	generation string
}

func (t requeueEngineTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == backfill.ActiveScopeGenerationQuery {
		return &queueFakeRows{rows: [][]any{{t.generation}}}, nil
	}
	return t.requeueWakeFakeTx.QueryContext(ctx, query, args...)
}

// publishWakes is the number of in-transaction waiting-wake statements one
// readiness publish runs (the coarse phase wake and the exact phase wake);
// each reopens the fake's scripted two rows.
const publishWakes = 2

func backwardEvidenceRows(now time.Time) []gpphase.PhaseState {
	return []gpphase.PhaseState{{
		Key: gpphase.PhaseKey{
			ScopeID: "scope-1", AcceptanceUnitID: "scope-1", SourceRunID: "gen-1",
			GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCrossRepoEvidence,
		},
		Phase: gpphase.PhaseBackwardEvidenceCommitted, CommittedAt: now, UpdatedAt: now,
	}}
}

// TestBackfillSeamPhaseStoreDefersRequeueUntilAfterCommit builds the phase
// store through the production seam (IngestionStore.backfillStore) and pins
// that its in-transaction wake holds the requeue count until the engine's
// AfterCommit hook runs.
func TestBackfillSeamPhaseStoreDefersRequeueUntilAfterCommit(t *testing.T) {
	t.Parallel()
	instruments, reader := newWakeMetrics(t)
	store := NewIngestionStore(&requeueWakeFake{reopened: 2})
	store.Instruments = instruments
	tx := requeueWakeFakeTx{&requeueWakeFake{reopened: 2}}

	phaseStore := store.backfillStore().NewPhaseStore(tx)
	hook, ok := phaseStore.(backfill.PostCommitHook)
	if !ok {
		t.Fatalf("seam phase store %T does not implement backfill.PostCommitHook", phaseStore)
	}
	if err := phaseStore.PublishGraphProjectionPhases(context.Background(), backwardEvidenceRows(time.Now())); err != nil {
		t.Fatalf("PublishGraphProjectionPhases: %v", err)
	}
	if got := waitingWakeRequeues(t, reader); got != 0 {
		t.Fatalf("requeue before AfterCommit = %d, want 0", got)
	}
	if got := waitingWakeOutcome(t, reader, telemetry.WaitingWakeOutcomeReopened); got < 1 {
		t.Fatalf("outcome reopened = %d, want the statement counted", got)
	}
	hook.AfterCommit(context.Background())
	if got := waitingWakeRequeues(t, reader); got != publishWakes*2 {
		t.Fatalf("requeue after AfterCommit = %d, want %d (2 reopened rows per in-tx wake)", got, publishWakes*2)
	}
}

// TestBackfillEngineRequeueCounterFollowsCommit drives the real fan-in over
// the production seam. A failed Commit is a real rollback: the engine never
// calls AfterCommit, so the requeue counter stays 0 while the outcome counter
// keeps the statement. A committed partition counts the reopened rows.
func TestBackfillEngineRequeueCounterFollowsCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		commitErr error
		want      int64
	}{
		{name: "committed counts reopened rows", want: publishWakes * 2},
		{name: "rolled back counts nothing", commitErr: errors.New("commit refused"), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := newWakeMetrics(t)
			fake := &requeueWakeFake{reopened: 2, commitErr: tt.commitErr}
			store := NewIngestionStore(requeueEngineDB{requeueWakeFake: fake, generation: "gen-1"})
			store.Instruments = instruments
			engine := store.backfillStore()
			// The post-commit re-wake is an autocommit wake that emits on its
			// own; drop it so only the in-transaction publish is measured.
			engine.WakePhaseWaitsAfterCommit = nil

			partition := backfill.ScopeGenerationPartition{ScopeID: "scope-1", GenerationID: "gen-1"}
			published, err := engine.PublishDeferredBackfillPartitions(context.Background(),
				map[backfill.ScopeGenerationPartition][]string{partition: {"repo-1"}}, nil, "", 1, nil)
			if (err != nil) != (tt.commitErr != nil) {
				t.Fatalf("PublishDeferredBackfillPartitions error = %v, want error %v", err, tt.commitErr != nil)
			}
			if tt.commitErr == nil && published != 1 {
				t.Fatalf("published = %d, want 1", published)
			}
			if got := waitingWakeRequeues(t, reader); got != tt.want {
				t.Fatalf("waiting_wake requeue = %d, want %d", got, tt.want)
			}
			if got := waitingWakeOutcome(t, reader, telemetry.WaitingWakeOutcomeReopened); got < 1 {
				t.Fatalf("outcome reopened = %d, want the statement counted even on rollback", got)
			}
		})
	}
}

// winnersWakeDB begins one scripted winners transaction.
type winnersWakeDB struct{ tx db.Transaction }

func (winnersWakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("the resweep must run inside its transaction")
}

func (d winnersWakeDB) Begin(context.Context) (db.Transaction, error) { return d.tx, nil }

// winnersWakeTx answers the winners-watermark wake with a scripted reopened
// count and forwards every other statement to the wrapped winners fake.
type winnersWakeTx struct {
	db.Transaction
	wake func() db.Rows
}

func (t winnersWakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == wakeWinnersWatermarkQuery {
		return t.wake(), nil
	}
	return t.Transaction.QueryContext(ctx, query, args...)
}

// TestWinnersResweepRequeueCounterFollowsCommit pins the two production winners
// transaction owners: the full resweep and the keyed tick open the deferral
// before their in-transaction wake and flush it only after Commit succeeds.
func TestWinnersResweepRequeueCounterFollowsCommit(t *testing.T) {
	t.Parallel()
	paths := []struct {
		name string
		tx   func(commitErr error, reopened int64) db.Transaction
		run  func(SupplyChainImpactWinnersStore) error
	}{
		{
			name: "full resweep",
			tx: func(commitErr error, reopened int64) db.Transaction {
				return winnersWakeTx{
					Transaction: &winnersFakeTx{claim: winnersClaimed(true), commitErr: commitErr},
					wake:        func() db.Rows { return winnersCounts(reopened, reopened) },
				}
			},
			run: func(s SupplyChainImpactWinnersStore) error {
				_, err := s.ResweepWinners(context.Background())
				return err
			},
		},
		{
			name: "keyed tick",
			tx: func(commitErr error, reopened int64) db.Transaction {
				return winnersWakeTx{
					Transaction: &phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100, commitErr: commitErr},
					wake:        func() db.Rows { return &phaseFakeRows{row: []any{reopened, reopened}} },
				}
			},
			run: func(s SupplyChainImpactWinnersStore) error {
				_, err := s.ResweepWinnersKeyed(context.Background())
				return err
			},
		},
	}
	outcomes := []struct {
		name      string
		commitErr error
		want      int64
	}{
		{name: "committed counts reopened rows", want: 3},
		{name: "rolled back counts nothing", commitErr: errors.New("commit failed"), want: 0},
	}
	for _, path := range paths {
		for _, outcome := range outcomes {
			t.Run(path.name+"/"+outcome.name, func(t *testing.T) {
				t.Parallel()
				instruments, reader := newWakeMetrics(t)
				store := NewSupplyChainImpactWinnersStore(winnersWakeDB{tx: path.tx(outcome.commitErr, 3)}).
					WithInstruments(instruments)
				err := path.run(store)
				if (err != nil) != (outcome.commitErr != nil) {
					t.Fatalf("resweep error = %v, want error %v", err, outcome.commitErr != nil)
				}
				if got := waitingWakeRequeues(t, reader); got != outcome.want {
					t.Fatalf("waiting_wake requeue = %d, want %d", got, outcome.want)
				}
				if got := waitingWakeOutcome(t, reader, telemetry.WaitingWakeOutcomeReopened); got < 1 {
					t.Fatalf("outcome reopened = %d, want the wake statement counted even on rollback", got)
				}
			})
		}
	}
}
