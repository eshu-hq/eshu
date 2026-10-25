// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// lostLockRun is a bootstrap-index run in miniature: a deferred pool whose hook
// shares a fence with the run lock, the lock pinned through that pool, the
// epoch opened, and deferred writes for the first three repositories.
type lostLockRun struct {
	ctx    context.Context
	db     *sql.DB
	config *pgx.ConnConfig
	pool   *sql.DB
	lock   *BulkLoadLock
	epoch  int64
}

func startLostLockRun(t *testing.T, idleSessionTimeout string) *lostLockRun {
	t.Helper()
	ctx, db, config := openDatabase(t)
	if idleSessionTimeout != "" {
		var name string
		if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `ALTER DATABASE "`+name+`" SET idle_session_timeout = '`+idleSessionTimeout+`'`); err != nil {
			t.Fatal(err)
		}
	}
	fence := NewRunFence(nil)
	pool := openFencedPool(t, config, fence)
	lock, err := AcquireBulkLoadLock(ctx, pool, LockOptions{Wait: 5 * time.Second, Fence: fence})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := BeginDeferral(ctx, postgres.SQLDB{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range testRepos[:3] {
		writeRecords(ctx, t, pool, repo, corpus(repo, 40))
	}
	return &lostLockRun{ctx: ctx, db: db, config: config, pool: pool, lock: lock, epoch: epoch}
}

func (r *lostLockRun) writeRest(t *testing.T) error {
	t.Helper()
	writer := postgres.NewContentWriter(postgres.SQLDB{DB: r.pool})
	for _, repo := range testRepos[3:] {
		if _, err := writer.Write(r.ctx, contentMaterialization(repo)); err != nil {
			return err
		}
	}
	return nil
}

// TestRepairerYieldsToLiveDeferredWritersAfterLockLossLive is the F1 trigger
// "the pinned lock session is terminated while deferred writers live on": the
// Repairer must not publish ready over the writers' underived rows. It skips on
// the deferred-writer lock, bootstrap finishes its writes and its own Finalize
// claims and publishes, and the table equals the derivation.
func TestRepairerYieldsToLiveDeferredWritersAfterLockLossLive(t *testing.T) {
	run := startLostLockRun(t, "")
	if _, err := run.db.ExecContext(run.ctx, `SELECT pg_terminate_backend($1)`, run.lock.pid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	f := &repairFixture{t: t, ctx: run.ctx, config: run.config, db: run.db, sqlDB: postgres.SQLDB{DB: run.db}, logs: &syncBuffer{}}
	outcome, err := f.repairer(RepairOptions{}).Tick(run.ctx)
	if err != nil || outcome != RepairSkippedLocked {
		t.Fatalf("Tick with live deferred writers = %s, %v; want skipped_locked", outcome, err)
	}
	skipped := f.events(t)["secret_lines.repair_skipped"]
	if len(skipped) != 1 || skipped[0]["reason"] != "deferred_writers" {
		t.Fatalf("repair_skipped events = %v, want one with reason deferred_writers", skipped)
	}
	if state, _ := stateRow(run.ctx, t, run.db); state != "not_built" {
		t.Fatalf("state = %s after the skip, want not_built (nothing claimed)", state)
	}

	if err := run.writeRest(t); err != nil {
		t.Fatalf("deferred writes after the Repairer's skip: %v", err)
	}
	result, err := Finalize(run.ctx, postgres.SQLDB{DB: run.db}, run.epoch, Options{})
	if err != nil || !result.Claimed || !result.Published {
		t.Fatalf("bootstrap Finalize = %+v, %v; want claimed and published", result, err)
	}
	requireParity(run.ctx, t, run.db, "after bootstrap finalize")
}

// TestDeferredSessionBirthIsFencedAfterLockLossLive is the second F1 trigger:
// idle_session_timeout kills the pinned session and every pool session, so no
// deferred session is alive when the Repairer ticks and it publishes for the
// content that exists. The pool then reconnects; the birth fence refuses the
// new session because the run's lock session is gone, so the write fails loudly
// instead of landing underived rows under a ready state.
func TestDeferredSessionBirthIsFencedAfterLockLossLive(t *testing.T) {
	run := startLostLockRun(t, "1500ms")
	time.Sleep(2500 * time.Millisecond)

	f := &repairFixture{t: t, ctx: run.ctx, config: run.config, db: run.db, sqlDB: postgres.SQLDB{DB: run.db}, logs: &syncBuffer{}}
	if _, err := f.repairer(RepairOptions{}).Tick(run.ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	err := run.writeRest(t)
	if err == nil || !errors.Is(err, ErrDeferredSessionRefused) && !strings.Contains(err.Error(), "secret_lines.deferred_session_refused") {
		t.Fatalf("deferred write after the lock session died = %v, want the birth fence error", err)
	}
	if ready, _ := Ready(run.ctx, run.db); ready {
		requireParity(run.ctx, t, run.db, "ready state after the fenced write")
	}
}
