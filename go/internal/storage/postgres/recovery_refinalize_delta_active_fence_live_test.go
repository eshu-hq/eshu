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
	maintenancestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
)

// Transaction and lock proofs for the #7797 reindex request: it runs inside
// the refinalize transaction, so it must roll back with it, and it must not
// add a lock cycle with the other users of repository_reindex_requests.

// reindexUpsertMarker identifies the reindex upsert statement.
const reindexUpsertMarker = "INSERT INTO repository_reindex_requests"

// afterReindexUpsertDB wraps the refinalize transaction and runs onExec on the
// first ExecContext issued after the reindex upsert. onExec returns the error
// that statement fails with, or nil to let it run.
type afterReindexUpsertDB struct {
	SQLDB
	onExec func() error

	mu       sync.Mutex
	upserted bool
	fired    bool
}

// Begin wraps the live transaction.
func (d *afterReindexUpsertDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &afterReindexUpsertTx{Transaction: tx, parent: d}, nil
}

func (d *afterReindexUpsertDB) state() (upserted, fired bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.upserted, d.fired
}

type afterReindexUpsertTx struct {
	db.Transaction
	parent *afterReindexUpsertDB
}

func (t *afterReindexUpsertTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, reindexUpsertMarker) {
		t.parent.mu.Lock()
		t.parent.upserted = true
		t.parent.mu.Unlock()
	}
	return t.Transaction.QueryContext(ctx, query, args...)
}

func (t *afterReindexUpsertTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t.parent.mu.Lock()
	trigger := t.parent.upserted && !t.parent.fired
	if trigger {
		t.parent.fired = true
	}
	t.parent.mu.Unlock()
	if trigger {
		if err := t.parent.onExec(); err != nil {
			return nil, err
		}
	}
	return t.Transaction.ExecContext(ctx, query, args...)
}

// TestRefinalizeDeltaActiveRollsBackReindexWithTheTransaction proves the
// reindex request and the projector re-enqueue commit or roll back together.
// A statement after the upsert fails, so the refinalize returns the error and
// leaves no watermark and no projector work behind.
func TestRefinalizeDeltaActiveRollsBackReindexWithTheTransaction(t *testing.T) {
	database, ctx := refinalizeDeltaActiveLiveDB(t)
	suffix := testSuffix(t)
	fixture := seedDeltaScope(t, ctx, database, "git-repository-scope:repo-rollback-"+suffix, suffix, "active", true)

	injected := errors.New("injected failure after the reindex upsert")
	wrapped := &afterReindexUpsertDB{SQLDB: SQLDB{DB: database}, onExec: func() error { return injected }}
	_, err := NewRecoveryStore(wrapped).RefinalizeScopeProjections(ctx,
		recovery.RefinalizeFilter{ScopeIDs: []string{fixture.scopeID}}, time.Now().UTC())
	if !errors.Is(err, injected) {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want the injected failure", err)
	}
	if upserted, fired := wrapped.state(); !upserted || !fired {
		t.Fatalf("upsert ran = %v, failure fired = %v: the test did not fail a statement after the upsert", upserted, fired)
	}
	if got := reindexWatermark(t, ctx, database, fixture.scopeID); !got.IsZero() {
		t.Fatalf("reindex watermark %s survived a rolled-back refinalize", got)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+fixture.scopeID+"_"+fixture.generationID); got != "" {
		t.Fatalf("projector work %q survived a rolled-back refinalize", got)
	}
}

// TestRefinalizeDeltaActiveReindexLockContention holds the refinalize
// transaction open right after its reindex upsert, while it also holds
// EXCLUSIVE on fact_work_items, and drives the two other users of
// repository_reindex_requests against it:
//
//   - the git collector's watermark read must not block, and must not see the
//     uncommitted row; and
//   - the admin reindex upsert of the same scope must wait on the row lock
//     (proved from pg_stat_activity), then succeed after the refinalize
//     commits, with no deadlock and the watermark never moved backward.
func TestRefinalizeDeltaActiveReindexLockContention(t *testing.T) {
	database, ctx := refinalizeDeltaActiveLiveDB(t)
	suffix := testSuffix(t)
	fixture := seedDeltaScope(t, ctx, database, "git-repository-scope:repo-contention-"+suffix, suffix, "active", true)

	held := make(chan struct{})
	release := make(chan struct{})
	wrapped := &afterReindexUpsertDB{SQLDB: SQLDB{DB: database}, onExec: func() error {
		close(held)
		<-release
		return nil
	}}
	refinalizeDone := make(chan error, 1)
	go func() {
		_, err := NewRecoveryStore(wrapped).RefinalizeScopeProjections(ctx,
			recovery.RefinalizeFilter{ScopeIDs: []string{fixture.scopeID}}, time.Now().UTC())
		refinalizeDone <- err
	}()
	select {
	case <-held:
	case err := <-refinalizeDone:
		t.Fatalf("refinalize finished before the hold point: %v", err)
	case <-ctx.Done():
		t.Fatal("refinalize never reached the hold point")
	}

	reindexStore := maintenancestore.NewRepositoryReindexStore(SQLDB{DB: database})
	readCtx, cancelRead := context.WithTimeout(ctx, 5*time.Second)
	watermarks, err := reindexStore.RepositoryReindexWatermarks(readCtx, time.Time{})
	cancelRead()
	if err != nil {
		close(release)
		t.Fatalf("collector watermark read blocked or failed while the refinalize held its locks: %v", err)
	}
	if _, ok := watermarks[fixture.scopeID]; ok {
		close(release)
		t.Fatal("collector read saw the uncommitted refinalize watermark")
	}

	adminDone := make(chan error, 1)
	go func() {
		_, err := reindexStore.RequestRepositoryReindex(ctx, []string{fixture.scopeID})
		adminDone <- err
	}()
	waited := waitForReindexLockWaiter(t, ctx, database)
	close(release)
	if !waited {
		t.Fatal("the admin reindex upsert never waited on the refinalize row lock; the test proved no contention")
	}

	if err := <-refinalizeDone; err != nil {
		t.Fatalf("refinalize under contention error = %v, want nil", err)
	}
	if err := <-adminDone; err != nil {
		t.Fatalf("admin reindex upsert under contention error = %v, want nil (no deadlock)", err)
	}
	if got := reindexWatermark(t, ctx, database, fixture.scopeID); got.IsZero() {
		t.Fatal("no reindex watermark after both writers committed")
	}
}

// waitForReindexLockWaiter polls pg_stat_activity until a reindex upsert in
// this database waits on a lock, for up to five seconds.
func waitForReindexLockWaiter(t *testing.T, ctx context.Context, database *sql.DB) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%`+reindexUpsertMarker+`%'`,
		).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
