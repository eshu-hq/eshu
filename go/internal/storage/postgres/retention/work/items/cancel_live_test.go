// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The coordinator counts a retention pass as canceled only when the pass
// error wraps context.Canceled and the pass context is done (#8829). These
// proofs pin the driver contract that rule rests on: a server-side lock or
// statement timeout surfaces as a *pgconn.PgError that is not
// context.Canceled, and a shutdown during a batch surfaces as
// context.Canceled.

// holdTableLock takes an ACCESS EXCLUSIVE lock on workflow_work_items in its
// own transaction, so the next batch statement waits on it, and returns the
// function that releases it. The test cleanup also releases it.
func holdTableLock(t *testing.T, ctx context.Context, database *sql.DB) func() {
	t.Helper()
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	release := func() { _ = holder.Rollback() }
	t.Cleanup(release)
	if _, err := holder.ExecContext(ctx, `LOCK TABLE workflow_work_items IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("hold table lock: %v", err)
	}
	return release
}

// assertServerTimeout fails unless err carries a *pgconn.PgError with the
// wanted SQLSTATE and does not match context.Canceled.
func assertServerTimeout(t *testing.T, err error, wantCode string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != wantCode {
		t.Fatalf("error = %v, want a *pgconn.PgError with SQLSTATE %s", err, wantCode)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v matches context.Canceled; a server-side timeout must not", err)
	}
}

func TestPruneWorkItemsLiveLockTimeoutIsAFailureNotACancellation(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedOldHistory(t, ctx, database)
	holdTableLock(t, ctx, database)

	policy := testPolicy(1000, 1)
	policy.LockTimeout = 200 * time.Millisecond
	_, err := NewSweeper(postgres.SQLDB{DB: database}).PruneWorkItems(ctx, liveAsOf, policy)

	assertServerTimeout(t, err, "55P03")
}

func TestPruneWorkItemsLiveShutdownDuringABatchIsACancellation(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedOldHistory(t, ctx, database)
	before := len(remainingIDs(t, ctx, database))
	release := holdTableLock(t, ctx, database)

	policy := testPolicy(1000, 1)
	policy.LockTimeout = 30 * time.Second
	passCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := time.AfterFunc(300*time.Millisecond, cancel)
	defer stop.Stop()
	started := time.Now()
	result, err := NewSweeper(postgres.SQLDB{DB: database}).PruneWorkItems(passCtx, liveAsOf, policy)
	elapsed := time.Since(started)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PruneWorkItems() error = %v, want it to wrap context.Canceled", err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		t.Fatalf("PruneWorkItems() error = %v carries SQLSTATE %s, want a plain cancellation", err, pgErr.Code)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("canceled pass took %s, want it to stop on cancel, not on the lock timeout", elapsed)
	}
	if result.Batches != 0 {
		t.Fatalf("result = %+v, want no committed batch", result)
	}
	release()
	if got := len(remainingIDs(t, ctx, database)); got != before {
		t.Fatalf("rows after a canceled batch = %d, want %d (the batch must roll back)", got, before)
	}
}

func TestLiveStatementTimeoutSurfacesAsAPgError(t *testing.T) {
	database, ctx := isolatedLiveDB(t)

	tx, err := postgres.SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	// The batch transaction's own timeout statement, with a statement
	// timeout short enough to fire.
	if _, err := tx.ExecContext(ctx, setBatchTimeoutsQuery, "1000ms", "100ms"); err != nil {
		t.Fatalf("set timeouts: %v", err)
	}
	_, err = tx.ExecContext(ctx, `SELECT pg_sleep(5)`)

	assertServerTimeout(t, err, "57014")
}
