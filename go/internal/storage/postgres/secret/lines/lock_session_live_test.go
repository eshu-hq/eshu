// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

func setIdleSessionTimeout(ctx context.Context, t *testing.T, f *repairFixture, value string) {
	t.Helper()
	var name string
	if err := f.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, `ALTER DATABASE "`+name+`" SET idle_session_timeout = '`+value+`'`); err != nil {
		t.Fatal(err)
	}
}

// TestBulkLoadLockSessionSurvivesIdleTimeoutLive: the pinned session sends a
// keep-alive, so a deployment whose idle_session_timeout is shorter than the run
// still releases the lock cleanly. Without the keep-alive the server ends the
// idle session with 57P05 and Release reports lost exclusivity.
func TestBulkLoadLockSessionSurvivesIdleTimeoutLive(t *testing.T) {
	f := newRepairFixture(t)
	setIdleSessionTimeout(f.ctx, t, f, "1500ms")
	pool := stdlib.OpenDB(*f.config)
	t.Cleanup(func() { _ = pool.Close() })
	lock, err := AcquireBulkLoadLock(f.ctx, pool, LockOptions{KeepAliveInterval: 400 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Second)
	if err := lock.Release(f.ctx); err != nil {
		t.Fatalf("Release after 5 s idle with idle_session_timeout=1500ms: %v", err)
	}
}

// TestRepairerPublishesOnAPoolOfOneLive: the reducer may run with
// ESHU_POSTGRES_MAX_OPEN_CONNS=1. The Repairer pins its lock on a dedicated
// one-connection pool, so Finalize still has the shared pool's only connection.
func TestRepairerPublishesOnAPoolOfOneLive(t *testing.T) {
	f := newRepairFixture(t)
	f.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	outcome, err := f.repairer(RepairOptions{}).Tick(ctx)
	if err != nil || outcome != RepairPublished {
		t.Fatalf("pool-of-one Tick = %s, %v; want published", outcome, err)
	}
	requireParity(f.ctx, t, f.db, "after a pool-of-one repair")
}

// TestBootstrapStartsDuringARebuildWaitsThenProceedsLive: a deferred session
// born while the Repairer rebuilds waits on (5318,2), then proceeds when the
// Repairer releases; AcquireBulkLoadLock then succeeds, with no error.
func TestBootstrapStartsDuringARebuildWaitsThenProceedsLive(t *testing.T) {
	f := newRepairFixture(t)
	inBatch, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	options := RepairOptions{}
	options.Finalize.afterBatch = func(ctx context.Context, _ string) error {
		once.Do(func() { close(inBatch) })
		select {
		case <-proceed:
		case <-ctx.Done():
		}
		return nil
	}
	ticked := make(chan RepairOutcome, 1)
	go func() {
		outcome, _ := f.repairer(options).Tick(f.ctx)
		ticked <- outcome
	}()
	select {
	case <-inBatch:
	case <-time.After(30 * time.Second):
		t.Fatal("the Repairer never reached its first batch")
	}

	fence := NewRunFence(nil)
	pool := openFencedPool(t, f.config, fence)
	born := make(chan error, 1)
	go func() {
		conn, err := pool.Conn(f.ctx)
		if err == nil {
			err = conn.Close()
		}
		born <- err
	}()
	select {
	case err := <-born:
		t.Fatalf("a deferred session was born during the rebuild (err %v); it must wait on (5318,2)", err)
	case <-time.After(700 * time.Millisecond):
	}

	close(proceed)
	if outcome := <-ticked; outcome != RepairPublished {
		t.Fatalf("Repairer Tick = %s, want published", outcome)
	}
	select {
	case err := <-born:
		if err != nil {
			t.Fatalf("the waiting session failed after the rebuild: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting session never proceeded after the rebuild")
	}
	lock, err := AcquireBulkLoadLock(f.ctx, pool, LockOptions{Wait: 5 * time.Second, Poll: 100 * time.Millisecond, Fence: fence})
	if err != nil {
		t.Fatalf("AcquireBulkLoadLock after the rebuild: %v", err)
	}
	if err := lock.Release(f.ctx); err != nil {
		t.Fatal(err)
	}
}

// TestUncertainLockSessionNeverReturnsToThePoolLive: a Release whose unlock
// reports the lock was not held leaves an uncertain session; it must be
// discarded, so the next connection from a pool of one is a different backend.
func TestUncertainLockSessionNeverReturnsToThePoolLive(t *testing.T) {
	f := newRepairFixture(t)
	// One connection that the pool keeps idle: the shape in which a returned
	// uncertain session would be handed to the next caller.
	pool := stdlib.OpenDB(*f.config)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	lock, err := AcquireBulkLoadLock(f.ctx, pool, LockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.conn.ExecContext(f.ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
		t.Fatal(err)
	}
	before := lock.pid
	if err := lock.Release(f.ctx); err == nil || !strings.Contains(err.Error(), "no longer held") {
		t.Fatalf("Release after the lock vanished = %v, want the lost-exclusivity error", err)
	}
	conn, err := pool.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var after int
	if err := conn.QueryRowContext(f.ctx, `SELECT pg_backend_pid()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatalf("the next connection is backend %d, the uncertain lock session; it must be discarded", after)
	}
}
