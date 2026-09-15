// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestRefinalizeRetirementBlocksReducerClaimBatchUntilCommit(t *testing.T) {
	for _, status := range []string{"pending", "retrying", "claimed", "running"} {
		t.Run(status, func(t *testing.T) {
			db, ctx := refinalizeRebuildResetLiveDB(t)
			suffix := testSuffix(t)
			scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)
			workItemID := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "claim-fence-"+status, status)
			if status == "claimed" || status == "running" {
				armLiveLease(t, ctx, db, workItemID, -time.Minute)
			}
			seedActiveRelationshipGeneration(t, ctx, db, activeGeneration, scopeID)

			retired := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseRecovery := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(releaseRecovery)
			racingDB := &refinalizeRetirementPauseDB{SQLDB: SQLDB{DB: db}, retired: retired, release: release}
			recoveryDone := make(chan error, 1)
			go func() {
				_, err := NewRecoveryStore(racingDB).RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
				recoveryDone <- err
			}()

			select {
			case <-retired:
			case err := <-recoveryDone:
				t.Fatalf("refinalize completed before retirement pause: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("refinalize did not reach retirement pause")
			}

			claimConn, err := db.Conn(ctx)
			if err != nil {
				t.Fatalf("open dedicated claim connection: %v", err)
			}
			defer func() { _ = claimConn.Close() }()
			var claimPID int
			if err := claimConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&claimPID); err != nil {
				t.Fatalf("read claim backend pid: %v", err)
			}
			queue := NewReducerQueue(claimFenceConn{Conn: claimConn}, "claim-fence-worker", time.Minute)
			queue.ClaimDomain = reducer.DomainCodeCallMaterialization
			claimDone := make(chan claimBatchResult, 1)
			go func() {
				intents, claimErr := queue.ClaimBatch(ctx, 1)
				claimDone <- claimBatchResult{intents: intents, err: claimErr}
			}()

			select {
			case result := <-claimDone:
				releaseRecovery()
				<-recoveryDone
				t.Fatalf("ClaimBatch returned before recovery commit: intents=%#v err=%v", result.intents, result.err)
			case <-time.After(250 * time.Millisecond):
			}
			assertBackendWaitingOnFactWorkItemsLock(t, ctx, db, claimPID, "RowExclusiveLock")

			releaseRecovery()
			if err := <-recoveryDone; err != nil {
				t.Fatalf("RefinalizeScopeProjections() error = %v", err)
			}
			if got := relationshipGenerationStatus(t, ctx, db, activeGeneration); got != "superseded" {
				t.Fatalf("relationship generation status = %q, want superseded", got)
			}
			select {
			case result := <-claimDone:
				if result.err != nil {
					t.Fatalf("ClaimBatch() error after recovery commit = %v", result.err)
				}
				if len(result.intents) != 1 || result.intents[0].IntentID != workItemID {
					t.Fatalf("ClaimBatch() intents = %#v, want only %q", result.intents, workItemID)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("ClaimBatch did not resume after recovery committed")
			}
		})
	}
}

func TestConcurrentRefinalizesSerializeWithoutDeadlock(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)
	seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "concurrent-fence", "succeeded")
	seedActiveRelationshipGeneration(t, ctx, db, activeGeneration, scopeID)

	locked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFirst)
	firstDone := make(chan error, 1)
	go func() {
		_, err := NewRecoveryStore(&refinalizeTableLockPauseDB{
			SQLDB: SQLDB{DB: db}, locked: locked, release: release,
		}).RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		firstDone <- err
	}()
	select {
	case <-locked:
	case err := <-firstDone:
		t.Fatalf("first refinalize completed before lock pause: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("first refinalize did not acquire EXCLUSIVE fence")
	}

	secondConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open second recovery connection: %v", err)
	}
	defer func() { _ = secondConn.Close() }()
	var secondPID int
	if err := secondConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatalf("read second recovery backend pid: %v", err)
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := NewRecoveryStore(claimFenceConn{Conn: secondConn}).RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC())
		secondDone <- err
	}()
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, db, secondPID, "ExclusiveLock")

	releaseFirst()
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("concurrent RefinalizeScopeProjections() error = %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent refinalizes did not serialize within 10s")
		}
	}
}

type claimBatchResult struct {
	intents []reducer.Intent
	err     error
}

type claimFenceConn struct{ *sql.Conn }

func (c claimFenceConn) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return c.Conn.QueryContext(ctx, query, args...)
}

type refinalizeRetirementPauseDB struct {
	SQLDB
	retired chan<- struct{}
	release <-chan struct{}
}

func (d *refinalizeRetirementPauseDB) Begin(ctx context.Context) (Transaction, error) {
	tx, err := d.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &refinalizeRetirementPauseTx{Transaction: tx, retired: d.retired, release: d.release}, nil
}

type refinalizeRetirementPauseTx struct {
	Transaction
	retired chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (t *refinalizeRetirementPauseTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := t.Transaction.ExecContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "UPDATE relationship_generations") {
		t.once.Do(func() { close(t.retired) })
		select {
		case <-t.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, err
}

type refinalizeTableLockPauseDB struct {
	SQLDB
	locked  chan<- struct{}
	release <-chan struct{}
}

func (d *refinalizeTableLockPauseDB) Begin(ctx context.Context) (Transaction, error) {
	tx, err := d.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &refinalizeTableLockPauseTx{Transaction: tx, locked: d.locked, release: d.release}, nil
}

type refinalizeTableLockPauseTx struct {
	Transaction
	locked  chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (t *refinalizeTableLockPauseTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := t.Transaction.ExecContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "LOCK TABLE fact_work_items") {
		t.once.Do(func() { close(t.locked) })
		select {
		case <-t.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, err
}

func assertBackendWaitingOnFactWorkItemsLock(t *testing.T, ctx context.Context, db *sql.DB, pid int, mode string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pg_locks
WHERE pid = $1
  AND relation = 'fact_work_items'::regclass
  AND mode = $2
  AND NOT granted`, pid, mode).Scan(&waiting)
		if err != nil {
			t.Fatalf("read fact_work_items lock wait: %v", err)
		}
		if waiting > 0 {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("backend pid %d has no ungranted fact_work_items %s", pid, mode)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
