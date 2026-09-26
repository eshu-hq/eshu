// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// secretLinesBatchUpsert upserts n keys of repo-c in key order (one statement,
// so both content_files triggers see the whole batch) with content that
// depends on variant: some variants carry findings, some do not, and the
// language alternates so a language-only change is exercised too.
const secretLinesBatchUpsert = `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-c', 'k_' || lpad(i::text, 4, '0') || '.go',
       CASE (i + $2::int) % 4
         WHEN 0 THEN 'package p' || E'\n' || 'token = "writer' || $2::int || 'value' || i || '"'
         WHEN 1 THEN 'password = "writer' || $2::int || 'pass' || i || '"' || E'\n' || 'secret: abcdef' || $2::int || i
         WHEN 2 THEN 'package clean' || $2::int
         ELSE 'AKIA' || lpad(i::text, 16, '0') || E'\n' || 'example password = "placeholder' || $2::int || '"'
       END,
       md5(i::text || $2::text), 2,
       CASE ($2::int) % 3 WHEN 0 THEN 'go' WHEN 1 THEN NULL ELSE 'python' END,
       now()
FROM generate_series(1, $1::int) AS i
ORDER BY 2
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash, line_count = EXCLUDED.line_count,
    language = EXCLUDED.language, indexed_at = EXCLUDED.indexed_at`

// secretLinesLockedDelete deletes a key range in key order, locking the
// content_files rows first so the only lock-order question left is the side
// table's.
const secretLinesLockedDelete = `
WITH doomed AS (
  SELECT repo_id, relative_path FROM content_files
  WHERE repo_id = 'repo-c' AND relative_path BETWEEN $1 AND $2
  ORDER BY repo_id, relative_path FOR UPDATE
)
DELETE FROM content_files f USING doomed d
WHERE f.repo_id = d.repo_id AND f.relative_path = d.relative_path`

// TestContentFileSecretLinesConcurrentWritersStayConsistentLive races three
// upserters over the same 300 keys with different content, one session that
// deletes key ranges, and one that runs the real retention prune, for several
// seconds. There must be no error and no deadlock, and the side table must
// equal the derivation afterwards.
func TestContentFileSecretLinesConcurrentWritersStayConsistentLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	db.SetMaxOpenConns(16)
	const keys = 300
	if _, err := db.ExecContext(ctx, secretLinesBatchUpsert, keys, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// One scope whose superseded generation names the pruner's key window.
	for _, stmt := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 VALUES ('scope-c', 'repository', 'git', 'scope-c', 'git', 'scope-c', now(), now(), 'active')`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
		 VALUES ('gen-c-old', 'scope-c', 'snapshot', now() - interval '3 days', now() - interval '3 days', 'superseded', now() - interval '2 days')`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
		 SELECT 'fact-c-' || i, 'scope-c', 'gen-c-old', 'file', 'fact-c-' || i, 'git', 'fact-c-' || i, now(), now(),
		        jsonb_build_object('repo_id', 'repo-c', 'relative_path', 'k_' || lpad(i::text, 4, '0') || '.go')
		 FROM generate_series(280, 300) AS i`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed scope: %v", err)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		runs = map[string]int{}
	)
	record := func(kind string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil && runCtx.Err() == nil {
			errs = append(errs, fmt.Errorf("%s: %w", kind, err))
			return
		}
		if err == nil {
			runs[kind]++
		}
	}
	for writer := 1; writer <= 3; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for variant := writer; runCtx.Err() == nil; variant += 3 {
				_, err := db.ExecContext(runCtx, secretLinesBatchUpsert, keys, variant)
				record("upsert", err)
			}
		}(writer)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; runCtx.Err() == nil; i++ {
			lo := 1 + (i*37)%250
			_, err := db.ExecContext(runCtx, secretLinesLockedDelete,
				fmt.Sprintf("k_%04d.go", lo), fmt.Sprintf("k_%04d.go", lo+30))
			record("delete", err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for runCtx.Err() == nil {
			_, err := execRowsAffected(runCtx, SQLDB{DB: db}, pruneContentFilesForGenerationsQuery, array.Of([]string{"gen-c-old"}))
			record("prune", err)
		}
	}()
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d concurrent statements failed; first: %v", len(errs), errors.Join(errs[:min(3, len(errs))]...))
	}
	for _, kind := range []string{"upsert", "delete", "prune"} {
		if runs[kind] < 3 {
			t.Fatalf("only %d %s statements completed; the race did not run (runs=%v)", runs[kind], kind, runs)
		}
	}
	requireSecretLinesParity(t, ctx, db, "after concurrent writers")
	t.Logf("concurrent statements completed: %v", runs)
}

// TestContentFileSecretLinesUpsertBlocksThenDerivesSecondWriterLive holds an
// open upsert transaction on a key and proves a concurrent upsert of the same
// key blocks on the content_files row lock, then derives its own findings from
// the content it committed over: the second writer's rows win, none of the
// first writer's survive.
func TestContentFileSecretLinesUpsertBlocksThenDerivesSecondWriterLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	const upsert = `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES ('repo-d', 'contended.go', $1, md5($1), 1, 'go', now())
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash, indexed_at = EXCLUDED.indexed_at`
	if _, err := db.ExecContext(ctx, upsert, `password = "seedseedseed1"`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	if _, err := first.ExecContext(ctx, upsert, `token = "firstwriter1234"`); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		// The comment lets the test find this backend in pg_stat_activity.
		_, err := db.ExecContext(ctx, "/* secret_lines_second_writer */ "+upsert, `secret = "secondwriter99"`)
		done <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE query LIKE '%secret_lines_second_writer%' AND query NOT LIKE '%pg_stat_activity%'
  AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 1 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("second upsert finished (%v) while the first transaction still held the row; it must block", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("second writer never showed a Lock wait; the contention proof is vacuous")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := first.Commit(); err != nil {
		t.Fatalf("commit first writer: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second writer: %v", err)
	}
	want := []string{"repo-d|contended.go|1|go|secret_literal|false"}
	if got := secretLinesSideRows(t, ctx, db); !slices.Equal(got, want) {
		t.Fatalf("side rows = %v, want only the second writer's %v", got, want)
	}
	requireSecretLinesParity(t, ctx, db, "after contended upsert")
}
