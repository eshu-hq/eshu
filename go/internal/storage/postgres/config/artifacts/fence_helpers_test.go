// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fenceTriggerNames are the four content_files version-fence triggers of
// migration 186 (#7126 protocol companion, section 2).
var fenceTriggerNames = []string{
	"content_files_version_fence_delete",
	"content_files_version_fence_insert",
	"content_files_version_fence_truncate",
	"content_files_version_fence_update",
}

// retentionCommitWaiter is the pg_stat_activity query text of a retention
// pass parked on a held version row: the commit-time fence (migration 186)
// takes version rows inside COMMIT, after every content DELETE. The pgx
// driver sends the commit in lower case.
const retentionCommitWaiter = "commit"

// mustExec runs each statement and fails the test on the first error.
func mustExec(ctx context.Context, t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("exec %.120q: %v", statement, err)
		}
	}
}

// versionOf returns the version row of repoID, or -1 when it has none.
func versionOf(ctx context.Context, t *testing.T, db *sql.DB, repoID string) int64 {
	t.Helper()
	var version int64
	err := db.QueryRowContext(ctx,
		`SELECT version FROM config_artifact_content_versions WHERE repo_id = $1`, repoID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return -1
	}
	if err != nil {
		t.Fatalf("read version of %s: %v", repoID, err)
	}
	return version
}

// versionsOf returns the version of each repository, -1 for a missing row.
func versionsOf(ctx context.Context, t *testing.T, db *sql.DB, repoIDs []string) map[string]int64 {
	t.Helper()
	out := make(map[string]int64, len(repoIDs))
	for _, repoID := range repoIDs {
		out[repoID] = versionOf(ctx, t, db, repoID)
	}
	return out
}

// seedZeroVersions sets version 0 for each repository, the shape migration 187
// seeds.
func seedZeroVersions(ctx context.Context, t *testing.T, db *sql.DB, repoIDs ...string) {
	t.Helper()
	for _, repoID := range repoIDs {
		mustExec(ctx, t, db, fmt.Sprintf(`
INSERT INTO config_artifact_content_versions (repo_id, version, bumped_xid, bumped_at)
VALUES ('%s', 0, '0'::xid8, now())
ON CONFLICT (repo_id) DO NOTHING`, repoID))
	}
}

// insertContentRows inserts n content_files rows per repository in one
// statement, paths prefix/f_<n>.go.
func insertContentRows(ctx context.Context, t *testing.T, db *sql.DB, prefix string, n int, repoIDs ...string) {
	t.Helper()
	mustExec(ctx, t, db, fmt.Sprintf(`
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT r, '%s/f_' || g || '.go', 'package main', md5(r || g::text), 1, 'go', now()
FROM unnest(ARRAY['%s']) AS r CROSS JOIN generate_series(1, %d) AS g`,
		prefix, strings.Join(repoIDs, "','"), n))
}

// fileMaterialization builds a content Write of files files under prefix for
// one repository.
func fileMaterialization(repoID, scopeID, prefix string, files int) content.Materialization {
	mat := content.Materialization{RepoID: repoID, ScopeID: scopeID, GenerationID: "gen-" + scopeID}
	for f := range files {
		mat.Records = append(mat.Records, content.Record{
			Path: fmt.Sprintf("%s/m%05d.tf", prefix, f),
			Body: fmt.Sprintf("resource \"aws_s3_bucket\" \"b%d\" {\n  bucket = \"x\"\n}\n", f),
		})
	}
	return mat
}

// newWriter returns the production content writer over db.
func newWriter(db *sql.DB) postgres.ContentWriter {
	return postgres.NewContentWriter(postgres.SQLDB{DB: db})
}

// writeContent runs the production ContentWriter.Write.
func writeContent(ctx context.Context, t *testing.T, db *sql.DB, mat content.Materialization) {
	t.Helper()
	if _, err := newWriter(db).Write(ctx, mat); err != nil {
		t.Fatalf("content write of %s: %v", mat.RepoID, err)
	}
}

// isDeadlock reports a PostgreSQL deadlock_detected error (SQLSTATE 40P01).
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// rowLockHolder holds row locks in an open transaction until release.
type rowLockHolder struct {
	tx *sql.Tx
}

// holdRows opens a transaction and runs lockSQL (a SELECT ... FOR UPDATE) in
// it, so later writers of those rows wait until release.
func holdRows(ctx context.Context, t *testing.T, db *sql.DB, lockSQL string, args ...any) *rowLockHolder {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := tx.ExecContext(ctx, lockSQL, args...); err != nil {
		_ = tx.Rollback()
		t.Fatalf("holder lock %.80q: %v", lockSQL, err)
	}
	holder := &rowLockHolder{tx: tx}
	t.Cleanup(func() { _ = holder.tx.Rollback() })
	return holder
}

// release ends the holder transaction and frees its locks.
func (h *rowLockHolder) release(t *testing.T) {
	t.Helper()
	if err := h.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("release holder: %v", err)
	}
}

// waitForLockWaiters polls pg_stat_activity until want backends in this
// database wait on a heavyweight lock with a query containing fragment.
func waitForLockWaiters(ctx context.Context, t *testing.T, db *sql.DB, fragment string, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'
  AND pid <> pg_backend_pid() AND strpos(query, $1) > 0`, fragment).Scan(&waiting); err != nil {
			t.Fatalf("poll lock waiters: %v", err)
		}
		if waiting >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, err := db.QueryContext(ctx, `
SELECT pid, state, coalesce(wait_event_type, ''), coalesce(wait_event, ''), left(query, 80)
FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`)
	if err == nil {
		for rows.Next() {
			var pid int
			var state, waitType, waitEvent, query string
			if rows.Scan(&pid, &state, &waitType, &waitEvent, &query) == nil {
				t.Logf("activity pid=%d state=%s wait=%s/%s query=%q", pid, state, waitType, waitEvent, query)
			}
		}
		_ = rows.Close()
	}
	t.Fatalf("timed out waiting for %d lock waiters on %q", want, fragment)
}

// retentionScope is one repository scope with a superseded generation whose
// file facts make every listed content_files path doomed for the prune.
type retentionScope struct {
	scopeID string
	repoID  string
	prefix  string
	files   int
}

// seedRetentionScopes builds the retention fixture of the S3 arm 3 shim for
// each scope: an active generation with no file facts, a superseded
// generation 9 days old with one file fact per content row, and the content
// rows themselves.
func seedRetentionScopes(ctx context.Context, t *testing.T, db *sql.DB, scopes []retentionScope) {
	t.Helper()
	for _, s := range scopes {
		mustExec(ctx, t, db, fmt.Sprintf(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('%[1]s', 'repository', 'github', 'acme/%[1]s', 'git', 'acme/%[1]s', now(), now(), 'active', '{}'::jsonb)`, s.scopeID),
			fmt.Sprintf(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-active-%[1]s', '%[1]s', 'snapshot', now(), now(), 'active')`, s.scopeID),
			fmt.Sprintf(`UPDATE ingestion_scopes SET active_generation_id = 'gen-active-%[1]s' WHERE scope_id = '%[1]s'`, s.scopeID),
			fmt.Sprintf(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ('gen-old-%[1]s', '%[1]s', 'snapshot', now() - interval '9 days', now() - interval '9 days',
        'superseded', now() - interval '8 days')`, s.scopeID),
			fmt.Sprintf(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'f-%[1]s-' || n, '%[1]s', 'gen-old-%[1]s', 'file', 'k-%[1]s-' || n, 'git', 'k-%[1]s-' || n, now(), now(),
       jsonb_build_object('repo_id', '%[2]s', 'relative_path', '%[3]s/f_' || n || '.go')
FROM generate_series(1, %[4]d) AS n`, s.scopeID, s.repoID, s.prefix, s.files))
		insertContentRows(ctx, t, db, s.prefix, s.files, s.repoID)
	}
}

// retentionPolicy admits every seeded superseded generation in one pass.
func retentionPolicy() postgres.GenerationRetentionPolicy {
	return postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 0, MaxSupersededAge: time.Hour,
		BatchGenerationLimit: 1000, BatchRowLimit: 10_000_000,
		PolicyScope: "global", PolicyRevision: "fence-proof",
	}
}

// prune runs the production GenerationRetentionStore pass.
func prune(ctx context.Context, db *sql.DB) (postgres.GenerationRetentionResult, error) {
	return postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: db}).
		PruneSupersededGenerations(ctx, retentionPolicy())
}

// contentCount returns the content_files rows of repoID under prefix.
func contentCount(ctx context.Context, t *testing.T, db *sql.DB, repoID, prefix string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM content_files WHERE repo_id = $1 AND relative_path LIKE $2 || '/%'`,
		repoID, prefix).Scan(&n); err != nil {
		t.Fatalf("count content: %v", err)
	}
	return n
}

// waitForSleeper polls pg_stat_activity until a backend whose query holds
// fragment is inside pg_sleep, so a test can act while that statement is
// mid-flight with its table locks held.
func waitForSleeper(ctx context.Context, t *testing.T, db *sql.DB, fragment string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var sleeping int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event = 'PgSleep'
  AND pid <> pg_backend_pid() AND strpos(query, $1) > 0`, fragment).Scan(&sleeping); err != nil {
			t.Fatalf("poll sleepers: %v", err)
		}
		if sleeping > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a sleeping %q statement", fragment)
}
