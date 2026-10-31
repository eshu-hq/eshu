// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func heldStaleStore(t *testing.T, db *fakeTransactionalDB) (IngestionStore, *telemetry.Instruments) {
	t.Helper()
	meter := sdkmetric.NewMeterProvider().Meter("held-stale-test")
	inst, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	store := NewIngestionStore(db)
	store.Instruments = inst
	return store, inst
}

// refuseOnce commits an empty successor over a 7-fact prior, which the guard
// refuses, and returns the scope's held-stale count for its collector kind.
func refuseOnce(t *testing.T, store IngestionStore, inst *telemetry.Instruments) int64 {
	t.Helper()
	tx := &fakeTx{}
	scopeValue, generation := successorBackstopFixture("generation-next", false)
	store.database = successorBackstopDB(tx, 7)
	store.beginner = store.database.(*fakeTransactionalDB)
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want a refusal", tx.rolledBack, tx.committed)
	}
	return inst.SuccessorHolds.HeldByKind()[string(scopeValue.CollectorKind)]
}

// TestRepeatedRefusalsHoldTheScopeStale proves the held-stale signal: one or
// two refusals are ordinary guard decisions, the third in a row holds the
// scope, and a commit that passes releases it.
func TestRepeatedRefusalsHoldTheScopeStale(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	store, inst := heldStaleStore(t, nil)

	for attempt := 1; attempt < telemetry.SuccessorHoldThreshold; attempt++ {
		if held := refuseOnce(t, store, inst); held != 0 {
			t.Fatalf("held scopes after %d refusals = %d, want 0", attempt, held)
		}
	}
	if held := refuseOnce(t, store, inst); held != 1 {
		t.Fatalf("held scopes after %d refusals = %d, want 1", telemetry.SuccessorHoldThreshold, held)
	}

	// A trusted successor passes the guard and ends the run.
	tx := &fakeTx{}
	scopeValue, generation := successorBackstopFixture("generation-trusted", true)
	db := successorBackstopDB(tx, 7)
	store.database, store.beginner = db, db
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration(trusted) error = %v, want nil", err)
	}
	if !tx.committed {
		t.Fatal("trusted successor committed = false, want true")
	}
	if held := inst.SuccessorHolds.HeldByKind()[string(scopeValue.CollectorKind)]; held != 0 {
		t.Fatalf("held scopes after a stored commit = %d, want 0", held)
	}
}

// TestUnchangedSkipReleasesAHeldScope proves a source that returned to the
// active content (same freshness hint) is no longer stale.
func TestUnchangedSkipReleasesAHeldScope(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	store, inst := heldStaleStore(t, nil)
	for attempt := 0; attempt < telemetry.SuccessorHoldThreshold; attempt++ {
		refuseOnce(t, store, inst)
	}
	scopeValue, generation := successorBackstopFixture("generation-same", false)
	generation.FreshnessHint = "hint-prior"
	tx := &fakeTx{}
	db := successorBackstopDB(tx, 7)
	store.database, store.beginner = db, db
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration(unchanged) error = %v, want nil", err)
	}
	if tx.committed {
		t.Fatal("unchanged snapshot committed = true, want a skip")
	}
	if held := inst.SuccessorHolds.HeldByKind()[string(scopeValue.CollectorKind)]; held != 0 {
		t.Fatalf("held scopes after an unchanged skip = %d, want 0", held)
	}
}

// commitFailingBeginner opens the fake transaction but fails its Commit, the
// shape of a lost claim fence or a commit-time conflict.
type commitFailingBeginner struct{ base *fakeTransactionalDB }

func (b commitFailingBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := b.base.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return commitFailingTx{Transaction: tx}, nil
}

type commitFailingTx struct{ db.Transaction }

func (commitFailingTx) Commit() error { return errors.New("commit failed") }

// TestHoldSurvivesAFailedCommit proves the hold ends only when a generation is
// durably committed. The guard passes a trusted successor before the commit, so
// clearing there would drop the hold when the commit then fails and nothing was
// stored (review F7).
func TestHoldSurvivesAFailedCommit(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	store, inst := heldStaleStore(t, nil)
	for attempt := 0; attempt < telemetry.SuccessorHoldThreshold; attempt++ {
		refuseOnce(t, store, inst)
	}

	scopeValue, generation := successorBackstopFixture("generation-trusted", true)
	failing := successorBackstopDB(&fakeTx{}, 7)
	store.database, store.beginner = failing, commitFailingBeginner{base: failing}
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(nil)); err == nil {
		t.Fatal("CommitScopeGeneration() error = nil, want the commit failure")
	}
	if held := inst.SuccessorHolds.HeldByKind()[string(scopeValue.CollectorKind)]; held != 1 {
		t.Fatalf("held scopes after a failed commit = %d, want 1: nothing was stored", held)
	}

	tx := &fakeTx{}
	ok := successorBackstopDB(tx, 7)
	store.database, store.beginner = ok, ok
	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if held := inst.SuccessorHolds.HeldByKind()[string(scopeValue.CollectorKind)]; held != 0 {
		t.Fatalf("held scopes after a durable commit = %d, want 0", held)
	}
}
