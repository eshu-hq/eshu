// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 ruling arb-8155-f1 2.7c commit-time fence contract: the fifth trigger (config_artifact_fence_pending_commit: row-level AFTER INSERT constraint trigger, deferrable, initially deferred, tgenabled O) on the UNLOGGED pending table keyed (bumped_xid, repo_id); bumps visible only after COMMIT; rollback, a rolled-back savepoint, SET CONSTRAINTS ALL IMMEDIATE followed by more writes, and COPY each bump exactly once or not at all; the pending table is empty after every commit and rollback; enrolled in the live-postgres-readiness runner; about 1 s per proof on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
)

// Commit-time fence contract (ruling arb-8155-f1, section 2.7c): the content
// triggers stage repositories into config_artifact_fence_pending, and one
// deferred constraint trigger bumps them at COMMIT and clears the stage.

// pendingRows counts every staged row, committed or not visible to others.
func pendingRows(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM config_artifact_fence_pending`).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	return n
}

// TestVersionFenceCommitTriggerIsDeferredLive pins the fifth fence trigger in
// pg_trigger: a row-level AFTER INSERT constraint trigger on the pending
// table, deferrable, initially deferred, enabled in origin and local
// replication roles ('O'), calling config_artifact_version_fence_commit; and
// the pending table is UNLOGGED with key (bumped_xid, repo_id).
func TestVersionFenceCommitTriggerIsDeferredLive(t *testing.T) {
	ctx, db := openDatabase(t)
	var (
		rowLevel, insert, constraint, deferrable, initDeferred bool
		enabled, function, persistence, key                    string
	)
	if err := db.QueryRowContext(ctx, `
SELECT (tg.tgtype & 1) <> 0, (tg.tgtype & 4) <> 0, tg.tgconstraint <> 0, tg.tgdeferrable,
       tg.tginitdeferred, tg.tgenabled::text, p.proname, c.relpersistence::text,
       pg_get_constraintdef(k.oid)
FROM pg_trigger tg
JOIN pg_proc p ON p.oid = tg.tgfoid
JOIN pg_class c ON c.oid = tg.tgrelid
JOIN pg_constraint k ON k.conrelid = c.oid AND k.contype = 'p'
WHERE tg.tgrelid = 'config_artifact_fence_pending'::regclass
  AND tg.tgname = 'config_artifact_fence_pending_commit'`).Scan(
		&rowLevel, &insert, &constraint, &deferrable, &initDeferred, &enabled, &function, &persistence, &key); err != nil {
		t.Fatalf("read the commit trigger: %v", err)
	}
	if !rowLevel || !insert || !constraint || !deferrable || !initDeferred || enabled != "O" ||
		function != "config_artifact_version_fence_commit" {
		t.Fatalf("commit trigger row=%v insert=%v constraint=%v deferrable=%v initdeferred=%v enabled=%s function=%s",
			rowLevel, insert, constraint, deferrable, initDeferred, enabled, function)
	}
	if persistence != "u" || key != "PRIMARY KEY (bumped_xid, repo_id)" {
		t.Fatalf("pending table persistence %q key %q, want UNLOGGED and PRIMARY KEY (bumped_xid, repo_id)", persistence, key)
	}
	var fenceTriggers int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_trigger
WHERE tgname LIKE 'content_files_version_fence_%' AND tgrelid = 'content_files'::regclass AND tgenabled = 'O'`).Scan(&fenceTriggers); err != nil {
		t.Fatal(err)
	}
	if fenceTriggers != len(fenceTriggerNames) {
		t.Fatalf("%d enabled content_files fence triggers, want %d", fenceTriggers, len(fenceTriggerNames))
	}
}

// TestVersionFenceBumpsOnceAtCommitLive covers the transaction shapes of
// 2.7c on one repository: a bump is visible only after COMMIT; a rollback
// bumps nothing; SET CONSTRAINTS ALL IMMEDIATE followed by more writes still
// bumps once; a write inside a rolled-back savepoint followed by a write
// outside it bumps once; COPY bumps once. The pending table is empty after
// every commit and every rollback.
func TestVersionFenceBumpsOnceAtCommitLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const repo = "repo-commit"
	seedZeroVersions(ctx, t, db, repo)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	run := func(statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				t.Fatalf("%.80q: %v", statement, err)
			}
		}
	}
	steps := []struct {
		name       string
		statements []string
		want       int64
	}{
		{"rollback", []string{"BEGIN", insertSQL(repo, "r/1"), "ROLLBACK"}, 0},
		{"immediate then more writes", []string{
			"BEGIN", insertSQL(repo, "i/1"), "SET CONSTRAINTS ALL IMMEDIATE",
			insertSQL(repo, "i/2"), "DELETE FROM content_files WHERE repo_id = '" + repo + "' AND relative_path = 'i/1'", "COMMIT",
		}, 1},
		{"savepoint rolled back, then a write", []string{
			"BEGIN", "SAVEPOINT s", insertSQL(repo, "s/1"),
			"ROLLBACK TO SAVEPOINT s", insertSQL(repo, "s/2"), "COMMIT",
		}, 1},
		{"savepoint rolled back alone", []string{
			"BEGIN", "SAVEPOINT s", insertSQL(repo, "s/3"),
			"ROLLBACK TO SAVEPOINT s", "COMMIT",
		}, 0},
	}
	for _, step := range steps {
		before := versionOf(ctx, t, db, repo)
		run(step.statements[:len(step.statements)-1]...)
		if got := versionOf(ctx, t, db, repo); got != before {
			t.Errorf("%s: version %d visible before COMMIT, want %d", step.name, got, before)
		}
		run(step.statements[len(step.statements)-1])
		if got := versionOf(ctx, t, db, repo) - before; got != step.want {
			t.Errorf("%s: version moved %d, want %d", step.name, got, step.want)
		}
		if n := pendingRows(ctx, t, db); n != 0 {
			t.Errorf("%s: %d pending rows after the transaction ended, want 0", step.name, n)
		}
	}

	before := versionOf(ctx, t, db, repo)
	if err := conn.Raw(func(driverConn any) error {
		_, err := driverConn.(*stdlib.Conn).Conn().PgConn().CopyFrom(ctx,
			strings.NewReader(repo+"\tc/1\tx\th\t1\t2026-10-16 00:00:00+00\n"+repo+"\tc/2\tx\th\t1\t2026-10-16 00:00:00+00\n"),
			"COPY content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at) FROM STDIN")
		return err
	}); err != nil {
		t.Fatalf("COPY: %v", err)
	}
	if got := versionOf(ctx, t, db, repo) - before; got != 1 {
		t.Fatalf("COPY of two rows moved the version %d, want 1", got)
	}
	if n := pendingRows(ctx, t, db); n != 0 {
		t.Fatalf("%d pending rows after COPY, want 0", n)
	}
}
