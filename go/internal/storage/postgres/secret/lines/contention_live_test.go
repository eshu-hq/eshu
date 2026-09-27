// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const heldUpdateSQL = `
UPDATE content_files
SET content = $3, content_hash = md5($3), language = 'go'
WHERE repo_id = $1 AND relative_path = $2`

// TestFinalizeYieldsToAHeldWriterLive proves the finalizer never makes a live
// writer wait and never deadlocks with it. A steady-state writer (triggers on)
// holds the LAST row of the only repository. The finalizer locks earlier rows
// FOR SHARE, blocks on the held one, hits lock_timeout, rolls back and retries.
// While it does, the writer updates an EARLIER row (the reverse lock order that
// would deadlock a finalizer that kept its locks): that update must complete
// promptly with no error. After the writer commits, the finalizer completes and
// the side table equals the derivation, including the writer's new findings.
func TestFinalizeYieldsToAHeldWriterLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	epoch := loadDeferred(ctx, t, db, config, 12)
	_ = config

	var first, last string
	if err := db.QueryRowContext(ctx, `
SELECT min(relative_path), max(relative_path) FROM content_files WHERE repo_id = 'repo-a'`).Scan(&first, &last); err != nil {
		t.Fatal(err)
	}

	writer, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, heldUpdateSQL, "repo-a", last, "package held\npassword = \"held-writer-value\"\n"); err != nil {
		t.Fatalf("hold last row: %v", err)
	}

	var (
		result Result
		final  error
		done   = make(chan struct{})
	)
	go func() {
		defer close(done)
		result, final = Finalize(ctx, sqlDB, epoch, Options{Workers: 1, BatchSize: 500, LockTimeout: 100 * time.Millisecond})
	}()
	// Let the finalizer lock earlier rows, block on the held one, and time out
	// several times.
	select {
	case <-done:
		t.Fatalf("Finalize completed while a writer held a row it needs: %+v, %v", result, final)
	case <-time.After(700 * time.Millisecond):
	}

	started := time.Now()
	if _, err := writer.ExecContext(ctx, heldUpdateSQL, "repo-a", first, "package held\ntoken: held-writer-first-value\n"); err != nil {
		t.Fatalf("writer's reverse-order update failed (deadlock or wait cycle with the finalizer): %v", err)
	}
	if waited := time.Since(started); waited > 900*time.Millisecond {
		t.Fatalf("writer waited %v for a row while the finalizer retried, want it to yield within lock_timeout", waited)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("writer commit: %v", err)
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Finalize did not complete after the writer committed")
	}
	if final != nil || !result.Published {
		t.Fatalf("Finalize = %+v, %v, want published", result, final)
	}
	if result.Retries == 0 {
		t.Fatal("Finalize recorded no retries although a writer held a row it needed")
	}
	requireParity(ctx, t, db, "after the held writer committed")
}

// TestFinalizeWithSteadyStateWritersLive races three ordinary-session upserters
// (triggers on) over the first repository's keys against a small-batch
// finalizer. Nothing may error or deadlock, and the finished table must equal
// the derivation: no lost or duplicated rows.
func TestFinalizeWithSteadyStateWritersLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	db.SetMaxOpenConns(16)
	sqlDB := postgres.SQLDB{DB: db}
	epoch := loadDeferred(ctx, t, db, config, 60)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		errs    []error
		upserts int
	)
	for writer := 1; writer <= 3; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for variant := writer; runCtx.Err() == nil; variant += 3 {
				if err := upsertVariant(runCtx, db, variant); err != nil && runCtx.Err() == nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("writer %d: %w", writer, err))
					mu.Unlock()
					return
				}
				mu.Lock()
				upserts++
				mu.Unlock()
			}
		}(writer)
	}

	result, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 3, BatchSize: 4, LockTimeout: 100 * time.Millisecond})
	cancel()
	wg.Wait()
	if err != nil {
		t.Fatalf("Finalize under steady-state writers: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("steady-state writers failed during the finalizer: %v", errs)
	}
	if !result.Published {
		t.Fatalf("Finalize did not publish: %+v", result)
	}
	t.Logf("finalizer retries=%d batches=%d against %d concurrent upserts", result.Retries, result.Batches, upserts)
	requireParity(ctx, t, db, "after Finalize raced steady-state writers")
}

// upsertVariant rewrites every repo-a key in key order (one statement, so both
// triggers see the whole batch) with content that depends on variant.
func upsertVariant(ctx context.Context, db *sql.DB, variant int) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT f.repo_id, f.relative_path,
       CASE (row_number() OVER (ORDER BY f.relative_path) + $1::int) % 3
         WHEN 0 THEN 'package w' || E'\n' || 'token = "writer' || $1::int || 'value"'
         WHEN 1 THEN 'package clean' || $1::int
         ELSE 'password = "writer' || $1::int || 'pass"' || E'\n' || 'secret: abcdef' || $1::int
       END,
       md5($1::text || f.relative_path), 2,
       CASE $1::int % 2 WHEN 0 THEN 'go' ELSE NULL END, now()
FROM content_files f WHERE f.repo_id = 'repo-a'
ORDER BY f.relative_path
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash,
    language = EXCLUDED.language, indexed_at = EXCLUDED.indexed_at`, variant)
	return err
}
