// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestRefinalizeFenceDoesNotConvoyUnrelatedQueueWrite(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	holderItem := seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration, "lock-holder", "succeeded")
	updateItem := seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration, "unrelated-update", "succeeded")
	seedActiveRelationshipGeneration(t, ctx, database, activeGeneration, scopeID)

	holderConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open lock-holder connection: %v", err)
	}
	defer func() { _ = holderConn.Close() }()
	var holderPID int
	if err := holderConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatalf("read lock-holder backend pid: %v", err)
	}
	holderTx, err := holderConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	if _, err := holderTx.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`, holderItem,
	); err != nil {
		t.Fatalf("hold ROW EXCLUSIVE on fact_work_items: %v", err)
	}

	recoveryConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open recovery connection: %v", err)
	}
	defer func() { _ = recoveryConn.Close() }()
	var recoveryPID int
	if err := recoveryConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&recoveryPID); err != nil {
		t.Fatalf("read recovery backend pid: %v", err)
	}
	lockRequested := make(chan struct{})
	recoveryDone := make(chan error, 1)
	go func() {
		_, refinalizeErr := NewRecoveryStore(refinalizeLockRequestDB{
			conn: recoveryConn, lockRequested: lockRequested, once: &sync.Once{},
		}).RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		recoveryDone <- refinalizeErr
	}()

	select {
	case <-lockRequested:
	case err := <-recoveryDone:
		t.Fatalf("refinalize returned before requesting the table fence: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("refinalize did not request the table fence")
	}
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, recoveryPID, "ExclusiveLock")

	updateConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open unrelated-writer connection: %v", err)
	}
	defer func() { _ = updateConn.Close() }()
	updateStarted := time.Now()
	updateDone := make(chan error, 1)
	go func() {
		_, updateErr := updateConn.ExecContext(ctx,
			`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`, updateItem,
		)
		updateDone <- updateErr
	}()

	// The acceptance bound is one second. The small allowance is for test-host
	// scheduling; the baseline path waits until the holder releases its lock.
	const maxUnrelatedWriteWait = 1500 * time.Millisecond
	var updateErr error
	select {
	case updateErr = <-updateDone:
	case <-time.After(maxUnrelatedWriteWait):
		if err := holderTx.Rollback(); err != nil && err != sql.ErrTxDone {
			t.Fatalf("release lock holder after convoy: %v", err)
		}
		select {
		case updateErr = <-updateDone:
		case <-time.After(5 * time.Second):
			t.Fatal("unrelated update stayed blocked after lock holder released")
		}
		if updateErr != nil {
			t.Fatalf("unrelated update after releasing holder: %v", updateErr)
		}
		select {
		case err := <-recoveryDone:
			if err != nil {
				t.Fatalf("RefinalizeScopeProjections() after releasing holder: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("refinalize did not finish after releasing the holder")
		}
		t.Fatalf("unrelated UPDATE waited %v behind the queued refinalize fence; want <= %v",
			time.Since(updateStarted), maxUnrelatedWriteWait)
	}
	if updateErr != nil {
		t.Fatalf("unrelated UPDATE error: %v", updateErr)
	}
	waited := time.Since(updateStarted)
	t.Logf("unrelated UPDATE completed in %s while the holder retained ROW EXCLUSIVE", waited)
	if waited > maxUnrelatedWriteWait {
		t.Fatalf("unrelated UPDATE waited %v, want <= %v", waited, maxUnrelatedWriteWait)
	}
	var holderStillGranted int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM pg_locks
WHERE pid = $1 AND relation = 'fact_work_items'::regclass
  AND mode = 'RowExclusiveLock' AND granted`, holderPID).Scan(&holderStillGranted); err != nil {
		t.Fatalf("verify lock holder stayed open through unrelated update: %v", err)
	}
	if holderStillGranted != 1 {
		t.Fatalf("holder ROW EXCLUSIVE lock count = %d, want 1 during unrelated update", holderStillGranted)
	}

	if err := holderTx.Rollback(); err != nil && err != sql.ErrTxDone {
		t.Fatalf("release lock holder: %v", err)
	}
	select {
	case err := <-recoveryDone:
		if err != nil {
			t.Fatalf("RefinalizeScopeProjections() after holder release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("refinalize did not finish after releasing the holder")
	}
}

func TestRefinalizeFenceRetryBudgetIncludesReducerDrain(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	workItemID := seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration, "retry-budget", "succeeded")

	holderConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open lock-holder connection: %v", err)
	}
	defer func() { _ = holderConn.Close() }()
	holderTx, err := holderConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	if _, err := holderTx.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`, workItemID,
	); err != nil {
		t.Fatalf("hold ROW EXCLUSIVE on fact_work_items: %v", err)
	}

	recoveryConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open recovery connection: %v", err)
	}
	defer func() { _ = recoveryConn.Close() }()
	var recoveryPID int
	if err := recoveryConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&recoveryPID); err != nil {
		t.Fatalf("read recovery backend pid: %v", err)
	}
	lockRequested := make(chan struct{})
	lockTimedOut := make(chan struct{}, 1)
	attemptStarted := make(chan struct{}, 2)
	recoveryDone := make(chan error, 1)
	store := NewRecoveryStore(refinalizeLockRequestDB{
		conn: recoveryConn, lockRequested: lockRequested, lockTimedOut: lockTimedOut,
		attemptStarted: attemptStarted, once: &sync.Once{},
	})
	store.refinalizeClaimFenceRetryBudget = 4 * time.Second
	proofCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	go func() {
		_, refinalizeErr := store.RefinalizeScopeProjections(proofCtx,
			recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		recoveryDone <- refinalizeErr
	}()
	select {
	case <-lockRequested:
	case err := <-recoveryDone:
		t.Fatalf("refinalize returned before requesting the table fence: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("refinalize did not request the table fence")
	}
	<-attemptStarted
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, recoveryPID, "ExclusiveLock")
	select {
	case <-lockTimedOut:
	case err := <-recoveryDone:
		t.Fatalf("refinalize returned before the fence timeout: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("refinalize fence did not time out")
	}
	if _, err := holderTx.ExecContext(ctx, `
		UPDATE fact_work_items
		SET status = 'claimed', lease_owner = 'retry-budget-proof', claim_until = now() + interval '1 hour'
		WHERE work_item_id = $1`, workItemID); err != nil {
		t.Fatalf("make reducer lease live after initial drain: %v", err)
	}
	if err := holderTx.Commit(); err != nil {
		t.Fatalf("commit live reducer lease: %v", err)
	}
	select {
	case <-attemptStarted:
	case err := <-recoveryDone:
		t.Fatalf("refinalize ended before its retry attempt: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("refinalize did not start a new transaction after 55P03")
	}
	select {
	case err := <-recoveryDone:
		if !errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
			t.Fatalf("RefinalizeScopeProjections() error = %v, want busy after the 4s retry budget", err)
		}
	case <-proofCtx.Done():
		t.Fatalf("refinalize did not apply the 4s retry budget: %v", proofCtx.Err())
	}
}

type refinalizeLockRequestDB struct {
	conn           *sql.Conn
	lockRequested  chan<- struct{}
	lockTimedOut   chan<- struct{}
	attemptStarted chan<- struct{}
	once           *sync.Once
}

func (d refinalizeLockRequestDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return d.conn.QueryContext(ctx, query, args...)
}

func (d refinalizeLockRequestDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.conn.ExecContext(ctx, query, args...)
}

func (d refinalizeLockRequestDB) Begin(ctx context.Context) (db.Transaction, error) {
	if d.attemptStarted != nil {
		d.attemptStarted <- struct{}{}
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &refinalizeLockRequestTx{
		Transaction:   SQLTx{Tx: tx},
		lockRequested: d.lockRequested,
		lockTimedOut:  d.lockTimedOut,
		once:          d.once,
	}, nil
}

type refinalizeLockRequestTx struct {
	db.Transaction
	lockRequested chan<- struct{}
	lockTimedOut  chan<- struct{}
	once          *sync.Once
}

func (t *refinalizeLockRequestTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "LOCK TABLE fact_work_items") {
		t.once.Do(func() { close(t.lockRequested) })
	}
	result, err := t.Transaction.ExecContext(ctx, query, args...)
	if err != nil && strings.Contains(query, "LOCK TABLE") && t.lockTimedOut != nil {
		select {
		case t.lockTimedOut <- struct{}{}:
		default:
		}
	}
	return result, err
}
