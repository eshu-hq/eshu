// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestFinalizeSurvivesAConcurrentKeyMoveLive proves a concurrent update that
// moves a locked row's primary key cannot make the finalizer skip files.
//
// PostgreSQL rechecks a row that a concurrent update changed while `ORDER BY ...
// LIMIT ... FOR SHARE` waited on it, and returns the NEW version at the OLD
// version's position, so the batch's keys come back out of order. If the moved
// row was the batch's last, a cursor taken from the last returned key jumps to
// the moved key and every unmoved file between the batch and that key is never
// derived, while ready is still published. Here the fifth file of the first
// batch moves to a key that sorts after every other file in the repository.
func TestFinalizeSurvivesAConcurrentKeyMoveLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	epoch := loadDeferred(ctx, t, db, config, 45)

	const batchSize = 5
	var fifth string
	if err := db.QueryRowContext(ctx, `
SELECT relative_path FROM content_files WHERE repo_id = 'repo-a'
ORDER BY relative_path OFFSET $1 LIMIT 1`, batchSize-1).Scan(&fifth); err != nil {
		t.Fatal(err)
	}

	mover, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mover.Rollback() }()
	if _, err := mover.ExecContext(ctx, `
UPDATE content_files SET relative_path = 'zzzz/moved.go'
WHERE repo_id = 'repo-a' AND relative_path = $1`, fifth); err != nil {
		t.Fatalf("hold the move of the batch's last key: %v", err)
	}

	var (
		result Result
		final  error
		done   = make(chan struct{})
	)
	go func() {
		defer close(done)
		// A long lock_timeout keeps the finalizer waiting on the held move
		// instead of yielding and retrying past it.
		result, final = Finalize(ctx, sqlDB, epoch, Options{
			Workers: 1, BatchSize: batchSize, LockTimeout: 30 * time.Second,
		})
	}()
	waitForBlockedFinalizer(ctx, t, db, done)
	if err := mover.Commit(); err != nil {
		t.Fatalf("commit the key move: %v", err)
	}
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("Finalize did not complete after the key move committed")
	}
	if final != nil || !result.Published {
		t.Fatalf("Finalize = %+v, %v, want published", result, final)
	}
	requireParity(ctx, t, db, "after a key move raced the finalizer")
}

// waitForBlockedFinalizer returns once a FOR SHARE statement is waiting on a
// row lock, so the test commits the holder only after the finalizer is blocked.
func waitForBlockedFinalizer(ctx context.Context, t *testing.T, db *sql.DB, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatal("Finalize completed while the key move was still held")
		default:
		}
		var blocked bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_stat_activity
  WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%FOR SHARE%'
)`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the finalizer never blocked on the held key move")
}
