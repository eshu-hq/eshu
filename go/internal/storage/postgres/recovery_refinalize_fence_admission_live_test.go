// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestRefinalizeFenceAdmissionDeadlineStopsAfterFenceAcquire(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration, "admission-budget", "succeeded")
	seedActiveRelationshipGeneration(t, ctx, database, activeGeneration, scopeID)

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
		`UPDATE fact_work_items SET updated_at = updated_at WHERE scope_id = $1`, scopeID,
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
	lockRequests := make(chan time.Time, 2)
	lockTimeouts := make(chan struct{}, 1)
	postFenceStarted := make(chan struct{})
	releasePostFence := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePostFence) }) }
	t.Cleanup(release)
	store := NewRecoveryStore(&fenceAdmissionPauseDB{
		conn: recoveryConn, lockRequests: lockRequests, lockTimeouts: lockTimeouts,
		postFenceStarted: postFenceStarted, releasePostFence: releasePostFence,
	})
	store.refinalizeClaimFenceRetryBudget = 3 * time.Second
	proofCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	recoveryDone := make(chan error, 1)
	go func() {
		_, refinalizeErr := store.RefinalizeScopeProjections(proofCtx,
			recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		recoveryDone <- refinalizeErr
	}()

	var firstFenceRequest time.Time
	select {
	case firstFenceRequest = <-lockRequests:
	case err := <-recoveryDone:
		t.Fatalf("refinalize completed before requesting its fence: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("refinalize did not request its fence")
	}
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, recoveryPID, "ExclusiveLock")
	select {
	case <-lockTimeouts:
	case err := <-recoveryDone:
		t.Fatalf("refinalize returned before its first fence timeout: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("first fence attempt did not hit lock_timeout")
	}
	if err := holderTx.Commit(); err != nil {
		t.Fatalf("release initial ROW EXCLUSIVE holder: %v", err)
	}
	select {
	case <-postFenceStarted:
	case err := <-recoveryDone:
		t.Fatalf("refinalize ended before its post-fence phase: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("retry did not reach post-fence writes")
	}

	deadline := firstFenceRequest.Add(store.refinalizeClaimFenceRetryBudget + 100*time.Millisecond)
	if wait := time.Until(deadline); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case err := <-recoveryDone:
			t.Fatalf("refinalize ended before the post-fence release: %v", err)
		case <-timer.C:
		}
	}
	release()
	select {
	case err := <-recoveryDone:
		if err != nil {
			t.Fatalf("RefinalizeScopeProjections() after fence admission = %v, want successful commit", err)
		}
	case <-proofCtx.Done():
		t.Fatalf("refinalize did not finish after releasing post-fence write: %v", proofCtx.Err())
	}
}

func TestRefinalizeFenceAdmissionPersistentHolderExhaustsWithoutConvoy(t *testing.T) {
	database, ctx, scopeID, activeGeneration, holderTx, recoveryConn, _ := fenceAdmissionLiveCase(t, "persistent-holder")
	defer func() { _ = recoveryConn.Close() }()
	defer func() { _ = holderTx.Rollback() }()
	requests := make(chan time.Time, 4)
	timeouts := make(chan struct{}, 4)
	store := NewRecoveryStore(&fenceAdmissionPauseDB{conn: recoveryConn, lockRequests: requests, lockTimeouts: timeouts})
	store.refinalizeClaimFenceRetryBudget = 3 * time.Second
	proofCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	done := refinalizeFenceAsync(store, proofCtx, scopeID)
	waitForInitialFenceTimeout(t, done, requests, timeouts)
	select {
	case err := <-done:
		if !errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
			t.Fatalf("persistent contention error = %v, want claim-fence busy", err)
		}
	case <-proofCtx.Done():
		t.Fatalf("persistent holder exceeded the admission budget: %v", proofCtx.Err())
	}
	updateItem := seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration,
		"unrelated-after-exhaustion", "succeeded")
	started := time.Now()
	if _, err := database.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`, updateItem,
	); err != nil {
		t.Fatalf("unrelated queue update while holder remains: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("unrelated queue update waited %s after fence exhaustion, want <= 1.5s", elapsed)
	}
}

func TestRefinalizeFenceAdmissionBoundsRetryBegin(t *testing.T) {
	_, ctx, scopeID, _, holderTx, recoveryConn, _ := fenceAdmissionLiveCase(t, "blocked-retry-begin")
	defer func() { _ = recoveryConn.Close() }()
	defer func() { _ = holderTx.Rollback() }()
	requests := make(chan time.Time, 2)
	timeouts := make(chan struct{}, 1)
	beginBlocked := make(chan struct{})
	store := NewRecoveryStore(&fenceAdmissionPauseDB{
		conn: recoveryConn, lockRequests: requests, lockTimeouts: timeouts,
		blockBeginAttempt: 2, beginBlocked: beginBlocked,
	})
	store.refinalizeClaimFenceRetryBudget = 3 * time.Second
	proofCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	done := refinalizeFenceAsync(store, proofCtx, scopeID)
	waitForInitialFenceTimeout(t, done, requests, timeouts)
	select {
	case <-beginBlocked:
	case err := <-done:
		t.Fatalf("retry did not reach transaction acquisition: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("retry transaction acquisition was not reached")
	}
	select {
	case err := <-done:
		if !errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
			t.Fatalf("blocked retry begin error = %v, want claim-fence busy", err)
		}
	case <-proofCtx.Done():
		t.Fatalf("retry Begin exceeded its admission deadline: %v", proofCtx.Err())
	}
	if got := store.database.(*fenceAdmissionPauseDB).beginAttempts.Load(); got != 2 {
		t.Fatalf("transaction begin attempts = %d, want 2", got)
	}
}

func TestRefinalizeFenceAdmissionCallerCancellation(t *testing.T) {
	for _, phase := range []string{"before_fence", "after_fence"} {
		t.Run(phase, func(t *testing.T) {
			database, ctx, scopeID, _, holderTx, recoveryConn, recoveryPID := fenceAdmissionLiveCase(t, "cancel-"+phase)
			defer func() { _ = recoveryConn.Close() }()
			defer func() { _ = holderTx.Rollback() }()
			requests := make(chan time.Time, 2)
			timeouts := make(chan struct{}, 1)
			postFenceStarted := make(chan struct{})
			releasePostFence := make(chan struct{})
			store := NewRecoveryStore(&fenceAdmissionPauseDB{
				conn: recoveryConn, lockRequests: requests, lockTimeouts: timeouts,
				postFenceStarted: postFenceStarted, releasePostFence: releasePostFence,
			})
			store.refinalizeClaimFenceRetryBudget = 4 * time.Second
			callerCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			done := refinalizeFenceAsync(store, callerCtx, scopeID)
			select {
			case <-requests:
			case err := <-done:
				t.Fatalf("refinalize ended before fence request: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("refinalize did not request fence")
			}
			assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, recoveryPID, "ExclusiveLock")
			if phase == "before_fence" {
				cancel()
				assertCallerCancellation(t, done)
				return
			}
			select {
			case <-timeouts:
			case err := <-done:
				t.Fatalf("refinalize ended before initial timeout: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("initial fence did not time out")
			}
			if err := holderTx.Commit(); err != nil {
				t.Fatalf("release fence holder: %v", err)
			}
			select {
			case <-postFenceStarted:
			case err := <-done:
				t.Fatalf("refinalize ended before post-fence phase: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("retry did not reach post-fence phase")
			}
			cancel()
			assertCallerCancellation(t, done)
		})
	}
}

func TestRefinalizeFenceAdmissionWriteAndCommitFailuresKeepTheirCauses(t *testing.T) {
	for _, phase := range []string{"write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			_, ctx, scopeID, _, holderTx, recoveryConn, _ := fenceAdmissionLiveCase(t, "failure-"+phase)
			defer func() { _ = recoveryConn.Close() }()
			defer func() { _ = holderTx.Rollback() }()
			injected := errors.New("injected post-fence " + phase + " failure")
			requests := make(chan time.Time, 2)
			timeouts := make(chan struct{}, 1)
			postFenceStarted := make(chan struct{})
			wrapper := &fenceAdmissionPauseDB{
				conn: recoveryConn, lockRequests: requests, lockTimeouts: timeouts,
				postFenceStarted: postFenceStarted,
			}
			if phase == "write" {
				wrapper.postFenceErr = injected
			} else {
				wrapper.commitErr = injected
			}
			store := NewRecoveryStore(wrapper)
			store.refinalizeClaimFenceRetryBudget = 4 * time.Second
			proofCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			done := refinalizeFenceAsync(store, proofCtx, scopeID)
			waitForInitialFenceTimeout(t, done, requests, timeouts)
			if err := holderTx.Commit(); err != nil {
				t.Fatalf("release fence holder: %v", err)
			}
			select {
			case <-postFenceStarted:
			case err := <-done:
				t.Fatalf("refinalize ended before post-fence failure: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("retry did not reach post-fence phase")
			}
			select {
			case err := <-done:
				if !errors.Is(err, injected) || errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
					t.Fatalf("RefinalizeScopeProjections() error = %v, want injected %s failure", err, phase)
				}
			case <-proofCtx.Done():
				t.Fatalf("post-fence %s failure was not returned: %v", phase, proofCtx.Err())
			}
			if got := wrapper.beginAttempts.Load(); got != 2 {
				t.Fatalf("transaction attempts = %d, want 2", got)
			}
		})
	}
}

func assertCallerCancellation(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
			t.Fatalf("RefinalizeScopeProjections() error = %v, want caller cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refinalize did not return after caller cancellation")
	}
}

func waitForInitialFenceTimeout(t *testing.T, done <-chan error, requests <-chan time.Time, timeouts <-chan struct{}) {
	t.Helper()
	select {
	case <-requests:
	case err := <-done:
		t.Fatalf("refinalize ended before requesting the fence: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("refinalize did not request the fence")
	}
	select {
	case <-timeouts:
	case err := <-done:
		t.Fatalf("refinalize ended before initial fence timeout: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("initial fence did not time out")
	}
}

func refinalizeFenceAsync(store RecoveryStore, ctx context.Context, scopeID string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := store.RefinalizeScopeProjections(ctx,
			recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		done <- err
	}()
	return done
}

func fenceAdmissionLiveCase(
	t *testing.T,
	entityKey string,
) (*sql.DB, context.Context, string, string, *sql.Tx, *sql.Conn, int) {
	t.Helper()
	database, ctx := refinalizeRebuildResetLiveDB(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, testSuffix(t))
	workItemID := seedRefinalizeResetReducerWork(t, ctx, database, scopeID, activeGeneration, entityKey, "succeeded")
	seedActiveRelationshipGeneration(t, ctx, database, activeGeneration, scopeID)
	holderConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open lock-holder connection: %v", err)
	}
	t.Cleanup(func() { _ = holderConn.Close() })
	holderTx, err := holderConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	if _, err := holderTx.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`, workItemID,
	); err != nil {
		t.Fatalf("hold ROW EXCLUSIVE on fact_work_items: %v", err)
	}
	recoveryConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open recovery connection: %v", err)
	}
	var recoveryPID int
	if err := recoveryConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&recoveryPID); err != nil {
		t.Fatalf("read recovery backend pid: %v", err)
	}
	return database, ctx, scopeID, activeGeneration, holderTx, recoveryConn, recoveryPID
}

type fenceAdmissionPauseDB struct {
	conn              *sql.Conn
	lockRequests      chan<- time.Time
	lockTimeouts      chan<- struct{}
	postFenceStarted  chan struct{}
	releasePostFence  <-chan struct{}
	postFenceErr      error
	commitErr         error
	blockBeginAttempt int32
	beginBlocked      chan struct{}
	beginBlockOnce    sync.Once
	beginAttempts     atomic.Int32
	once              sync.Once
}

func (d *fenceAdmissionPauseDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return d.conn.QueryContext(ctx, query, args...)
}

func (d *fenceAdmissionPauseDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.conn.ExecContext(ctx, query, args...)
}

func (d *fenceAdmissionPauseDB) Begin(ctx context.Context) (db.Transaction, error) {
	attempt := d.beginAttempts.Add(1)
	if int32(attempt) == d.blockBeginAttempt && d.beginBlocked != nil {
		d.beginBlockOnce.Do(func() { close(d.beginBlocked) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &fenceAdmissionPauseTx{
		Transaction: SQLTx{Tx: tx}, db: d,
	}, nil
}

type fenceAdmissionPauseTx struct {
	db.Transaction
	db *fenceAdmissionPauseDB
}

func (t *fenceAdmissionPauseTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "LOCK TABLE fact_work_items") {
		t.db.lockRequests <- time.Now()
	}
	result, err := t.Transaction.ExecContext(ctx, query, args...)
	if strings.Contains(query, "LOCK TABLE") && err != nil {
		select {
		case t.db.lockTimeouts <- struct{}{}:
		default:
		}
	}
	if err == nil && strings.Contains(query, "UPDATE relationship_generations") {
		if t.db.postFenceStarted != nil {
			t.db.once.Do(func() { close(t.db.postFenceStarted) })
		}
		if t.db.postFenceErr != nil {
			return nil, t.db.postFenceErr
		}
		if t.db.releasePostFence != nil {
			select {
			case <-t.db.releasePostFence:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return result, err
}

func (t *fenceAdmissionPauseTx) Commit() error {
	if t.db.commitErr != nil {
		return t.db.commitErr
	}
	return t.Transaction.Commit()
}
