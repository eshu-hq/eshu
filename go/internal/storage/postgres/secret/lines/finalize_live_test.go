// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

var testRepos = []string{"repo-a", "repo-b", "repo-c", "repo-d", "repo-e", "repo-f"}

// loadDeferred does what bootstrap-index does around collection: BeginDeferral,
// then content writes from a deferred session. It returns the epoch.
func loadDeferred(ctx context.Context, t *testing.T, db *sql.DB, config *pgx.ConnConfig, filesPerRepo int) int64 {
	t.Helper()
	epoch, err := BeginDeferral(ctx, postgres.SQLDB{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	pool := openDeferredPool(t, config)
	for _, repo := range testRepos {
		writeRecords(ctx, t, pool, repo, corpus(repo, filesPerRepo))
	}
	return epoch
}

// TestFinalizeRebuildsSideTableToParityLive proves the finalizer converges the
// side table to the derivation after a deferred load: rows missing for files a
// deferred insert wrote, and stale rows left by a deferred rewrite of files an
// earlier ordinary write had derived. Small batches force keyset paging.
func TestFinalizeRebuildsSideTableToParityLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	// An earlier ordinary write leaves rows the deferred rewrite will make stale.
	writeRecords(ctx, t, db, "repo-a", corpus("repo-a", 20))
	if sideRowCount(ctx, t, db) == 0 {
		t.Fatal("setup: ordinary write derived nothing")
	}
	epoch, err := BeginDeferral(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	pool := openDeferredPool(t, config)
	rewritten := corpus("repo-a", 20)
	for i := range rewritten {
		rewritten[i].Body = "package rewritten\nclient_secret = \"rewritten-" + fmt.Sprint(i) + "-value\"\n"
	}
	writeRecords(ctx, t, pool, "repo-a", rewritten)
	for _, repo := range testRepos[1:] {
		writeRecords(ctx, t, pool, repo, corpus(repo, 45))
	}

	var files int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files`).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if missing, extra := parityCounts(ctx, t, db); missing == 0 || extra == 0 {
		t.Fatalf("setup: parity before finalize = missing %d extra %d, want both > 0", missing, extra)
	}
	if ready, err := Ready(ctx, db); err != nil || ready {
		t.Fatalf("Ready() before Finalize = %v, %v, want false", ready, err)
	}

	result, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 3, BatchSize: 7})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if !result.Claimed || !result.Published || result.Repositories != len(testRepos) || result.Files != files {
		t.Fatalf("Finalize result = %+v, want claimed, published, %d repositories, %d files", result, len(testRepos), files)
	}
	if result.Batches < int64(len(testRepos)) || result.Batches <= files/7 {
		t.Fatalf("Finalize ran %d batches over %d files at size 7, want keyset paging (> %d)", result.Batches, files, files/7)
	}
	requireParity(ctx, t, db, "after Finalize")
	if state, got := stateRow(ctx, t, db); state != StateReady || got != epoch {
		t.Fatalf("state after Finalize = %q epoch %d, want ready epoch %d", state, got, epoch)
	}
	if ready, err := Ready(ctx, db); err != nil || !ready {
		t.Fatalf("Ready() after Finalize = %v, %v, want true", ready, err)
	}

	// A finished epoch claims nothing: the rerun is a no-op.
	again, err := Finalize(ctx, sqlDB, epoch, Options{})
	if err != nil || again.Claimed {
		t.Fatalf("Finalize on a ready epoch = %+v, %v, want unclaimed", again, err)
	}
}

func parityCounts(ctx context.Context, t *testing.T, db *sql.DB) (missing, extra int64) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
WITH derived AS (
  SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, '') AS language, d.finding_kind, d.line_text
  FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
), side AS (
  SELECT repo_id, relative_path, line_number, language, finding_kind, line_text FROM content_file_secret_lines
)
SELECT (SELECT count(*) FROM (SELECT * FROM derived EXCEPT ALL SELECT * FROM side) a),
       (SELECT count(*) FROM (SELECT * FROM side EXCEPT ALL SELECT * FROM derived) b)`).Scan(&missing, &extra); err != nil {
		t.Fatal(err)
	}
	return missing, extra
}

// TestFinalizeIsRestartSafeLive kills the finalizer after a few committed
// batches, proves the failure is durable and readers stay off the side table,
// then reruns the same epoch to a clean, ready, parity-equal table.
func TestFinalizeIsRestartSafeLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	epoch := loadDeferred(ctx, t, db, config, 45)

	injected := errors.New("injected crash after three batches")
	batches := 0
	_, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 1, BatchSize: 5, afterBatch: func(context.Context, string) error {
		batches++
		if batches == 3 {
			return injected
		}
		return nil
	}})
	if !errors.Is(err, injected) {
		t.Fatalf("Finalize error = %v, want the injected crash", err)
	}
	if state, _ := stateRow(ctx, t, db); state != StateFailed {
		t.Fatalf("state after a crashed Finalize = %q, want failed", state)
	}
	if ready, err := Ready(ctx, db); err != nil || ready {
		t.Fatalf("Ready() after a crashed Finalize = %v, %v, want false", ready, err)
	}
	if missing, _ := parityCounts(ctx, t, db); missing == 0 {
		t.Fatal("setup: the crashed finalizer somehow finished the table")
	}

	result, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 2, BatchSize: 5})
	if err != nil || !result.Published {
		t.Fatalf("restarted Finalize = %+v, %v, want published", result, err)
	}
	requireParity(ctx, t, db, "after the restarted Finalize")
	if state, _ := stateRow(ctx, t, db); state != StateReady {
		t.Fatalf("state after restart = %q, want ready", state)
	}
}

// TestFinalizeIsSupersededByNewBulkLoadLive proves the epoch fence: a bulk load
// that begins while a finalizer runs keeps readers off the side table, and the
// older finalizer neither errors nor publishes ready.
func TestFinalizeIsSupersededByNewBulkLoadLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	epoch := loadDeferred(ctx, t, db, config, 20)

	superseded := false
	result, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 1, BatchSize: 5, afterBatch: func(ctx context.Context, _ string) error {
		if !superseded {
			superseded = true
			_, err := BeginDeferral(ctx, sqlDB)
			return err
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if result.Published {
		t.Fatal("a finalizer published ready for an epoch a newer bulk load had replaced")
	}
	if state, got := stateRow(ctx, t, db); state != StateNotBuilt || got != epoch+1 {
		t.Fatalf("state = %q epoch %d, want not_built epoch %d", state, got, epoch+1)
	}
	if ready, err := Ready(ctx, db); err != nil || ready {
		t.Fatalf("Ready() = %v, %v, want false", ready, err)
	}
}
