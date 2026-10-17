// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 (#7126 PR3b-1b) disposable PostgreSQL 18 proof of the migration 186 content_files version fence: INSERT, UPDATE, upsert, DELETE and TRUNCATE each bump a touched repository once per transaction and leave others alone; a rolled-back write leaves no version row; two concurrent first writes of an unseeded repository converge on one row at version 2 with no error (10 rounds); a transaction whose id a restored version row already holds still bumps once (the (bumped_xid, bumped_at) guard); the fence is the last statement-level AFTER trigger per event, after the migration 138 secret-lines triggers and, for DELETE, after the row-level RI triggers; enrolled in the live-postgres-readiness runner; 0.7-1.5 s per proof on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

// TestVersionFenceBumpsOncePerRepositoryPerTransactionLive drives every
// fenced event (INSERT, UPDATE, upsert, DELETE, TRUNCATE) through
// multi-repository statements, alone and twice in one transaction, and
// asserts each touched repository's version moves by exactly one per
// transaction while an untouched repository does not move. An upsert fires
// both the INSERT and the UPDATE trigger; the xid guard keeps it at one.
func TestVersionFenceBumpsOncePerRepositoryPerTransactionLive(t *testing.T) {
	ctx, db := openDatabase(t)
	repos := []string{"repo-a", "repo-b", "repo-c"}
	seedZeroVersions(ctx, t, db, append(repos, "repo-idle")...)

	steps := []struct {
		name       string
		statements []string
		touched    []string
	}{
		{"insert", []string{`
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT r, 'p/' || g, 'x', 'h', 1, now()
FROM unnest(ARRAY['repo-a', 'repo-b', 'repo-c']) AS r CROSS JOIN generate_series(1, 4) AS g`}, repos},
		{
			"update",
			[]string{`UPDATE content_files SET content = 'y' WHERE repo_id IN ('repo-a', 'repo-b')`},
			[]string{"repo-a", "repo-b"},
		},
		{
			"upsert_conflict",
			[]string{`
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-a', 'p/1', 'z', 'h', 1, now()), ('repo-c', 'p/new', 'z', 'h', 1, now())
ON CONFLICT (repo_id, relative_path) DO UPDATE SET content = EXCLUDED.content`},
			[]string{"repo-a", "repo-c"},
		},
		{"two_statements_one_transaction", []string{
			"BEGIN",
			`UPDATE content_files SET content = 'w' WHERE repo_id = 'repo-b'`,
			`DELETE FROM content_files WHERE repo_id = 'repo-b' AND relative_path = 'p/1'`,
			`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-b', 'p/9', 'w', 'h', 1, now())`,
			"COMMIT",
		}, []string{"repo-b"}},
		{"delete", []string{`DELETE FROM content_files WHERE relative_path = 'p/2'`}, repos},
		{"truncate", []string{`TRUNCATE content_files CASCADE`}, append(repos, "repo-idle")},
	}
	for _, step := range steps {
		before := versionsOf(ctx, t, db, append(repos, "repo-idle"))
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range step.statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				t.Fatalf("%s: %v", step.name, err)
			}
		}
		_ = conn.Close()
		after := versionsOf(ctx, t, db, append(repos, "repo-idle"))
		touched := map[string]bool{}
		for _, r := range step.touched {
			touched[r] = true
		}
		for repo, version := range after {
			want := before[repo]
			if touched[repo] {
				want++
			}
			if version != want {
				t.Errorf("%s: %s version = %d, want %d (before %d)", step.name, repo, version, want, before[repo])
			}
		}
	}
}

// TestVersionFenceCreatesMissingRowOnFirstWriteLive covers a repository with
// no seeded version row: a first Write creates it at version 1, and a
// ROLLBACK leaves no row because the bump commits with the content.
func TestVersionFenceCreatesMissingRowOnFirstWriteLive(t *testing.T) {
	ctx, db := openDatabase(t)
	mustExec(ctx, t, db, "BEGIN; "+insertSQL("repo-rolled", "p/1")+"; ROLLBACK")
	if got := versionOf(ctx, t, db, "repo-rolled"); got != -1 {
		t.Fatalf("rolled-back write left version %d, want no row", got)
	}
	writeContent(ctx, t, db, fileMaterialization("repo-new", "scope-new", "a", 3))
	if got := versionOf(ctx, t, db, "repo-new"); got != 1 {
		t.Fatalf("first Write version = %d, want 1", got)
	}
}

// TestVersionFenceConcurrentFirstWritesConvergeLive is ruling addition 3c:
// two sessions write a repository that has no version row at the same time.
// The fence inserts the version row at COMMIT, so the first session runs SET
// CONSTRAINTS ALL IMMEDIATE to put its insert in flight before the second
// commits. The second's commit-time insert waits on the first's uncommitted
// row and resolves through ON CONFLICT: one row, no error, version 2.
func TestVersionFenceConcurrentFirstWritesConvergeLive(t *testing.T) {
	ctx, db := openDatabase(t)
	for round := range 10 {
		repo := "repo-race-" + string(rune('a'+round))
		first, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := first.ExecContext(ctx, insertSQL(repo, "x/1")); err != nil {
			t.Fatal(err)
		}
		if _, err := first.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var secondErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, secondErr = db.ExecContext(ctx, insertSQL(repo, "y/1"))
		}()
		waitForLockWaiters(ctx, t, db, repo, 1)
		if err := first.Commit(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if secondErr != nil {
			t.Fatalf("round %d: second first-write failed: %v", round, secondErr)
		}
		var rows int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM config_artifact_content_versions WHERE repo_id = $1`, repo).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if got := versionOf(ctx, t, db, repo); rows != 1 || got != 2 {
			t.Fatalf("round %d: %d version rows at version %d, want 1 row at version 2", round, rows, got)
		}
	}
}

// insertSQL is a one-row content_files insert for repoID at path.
func insertSQL(repoID, path string) string {
	return `INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('` + repoID + `', '` + path + `', 'x', 'h', 1, now())`
}

// TestVersionFenceTriggersFireLastLive is the trigger-order schema test
// (S3 ruling; ruling addition 3d). PostgreSQL fires row-level AFTER triggers
// before statement-level AFTER triggers, and triggers of one level in name
// order. For each event the fence must be the last statement-level AFTER
// trigger, the migration 138 secret-lines triggers must be present and fire
// before it, and the migration 138 foreign key's row-level RI triggers on
// content_files must fire before the DELETE fence.
func TestVersionFenceTriggersFireLastLive(t *testing.T) {
	ctx, db := openDatabase(t)
	type trigger struct {
		name     string
		rowLevel bool
		internal bool
		transOld bool
		transNew bool
	}
	events := map[string]int{"insert": 4, "delete": 8, "update": 16, "truncate": 32}
	for event, bit := range events {
		rows, err := db.QueryContext(ctx, `
SELECT tgname, (tgtype & 1) <> 0, tgisinternal, tgoldtable IS NOT NULL, tgnewtable IS NOT NULL
FROM pg_trigger
WHERE tgrelid = 'content_files'::regclass AND (tgtype & 2) = 0 AND (tgtype & 64) = 0
  AND (tgtype & $1) <> 0 AND tgenabled IN ('O', 'A')
ORDER BY (tgtype & 1) = 0, tgname COLLATE "C"`, bit)
		if err != nil {
			t.Fatal(err)
		}
		var order []trigger
		for rows.Next() {
			var tr trigger
			if err := rows.Scan(&tr.name, &tr.rowLevel, &tr.internal, &tr.transOld, &tr.transNew); err != nil {
				t.Fatal(err)
			}
			order = append(order, tr)
		}
		_ = rows.Close()
		names := make([]string, 0, len(order))
		for _, tr := range order {
			names = append(names, tr.name)
		}
		want := "content_files_version_fence_" + event
		if len(order) == 0 || order[len(order)-1].name != want {
			t.Fatalf("%s AFTER firing order %v: want %s last", event, names, want)
		}
		fence := order[len(order)-1]
		if fence.rowLevel {
			t.Errorf("%s fence is row-level, want statement-level", event)
		}
		switch event {
		case "insert", "update":
			if !fence.transNew || fence.transOld {
				t.Errorf("%s fence transition tables old=%v new=%v, want NEW TABLE only", event, fence.transOld, fence.transNew)
			}
			if !containsName(names[:len(names)-1], "content_files_secret_lines_"+event) {
				t.Errorf("%s firing order %v lacks migration 138 trigger before the fence", event, names)
			}
		case "delete":
			if !fence.transOld || fence.transNew {
				t.Errorf("delete fence transition tables old=%v new=%v, want OLD TABLE only", fence.transOld, fence.transNew)
			}
			ri := 0
			for _, tr := range order[:len(order)-1] {
				if tr.internal && tr.rowLevel {
					ri++
				}
			}
			if ri == 0 {
				t.Errorf("delete firing order %v: no row-level RI trigger of the migration 138 foreign key before the fence", names)
			}
		}
	}
}

// containsName reports whether names holds name.
func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// dropFence removes the four content_files fence triggers so a test can install a seeded
// variant; the functions stay.
func dropFence(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range fenceTriggerNames {
		mustExec(ctx, t, db, "DROP TRIGGER IF EXISTS "+name+" ON content_files")
	}
}

// TestVersionFenceSurvivesATransactionIDRestartLive covers the PR3b-1a review
// forward note (README, "Known limits for the trigger"): after a logical
// restore the transaction id counter restarts while stored bumped_xid values
// survive, so a later transaction can receive an id a version row already
// holds. The guard compares (bumped_xid, bumped_at), so that transaction
// still bumps once, and a second statement in it does not bump again.
func TestVersionFenceSurvivesATransactionIDRestartLive(t *testing.T) {
	ctx, db := openDatabase(t)
	seedZeroVersions(ctx, t, db, "repo-restored")
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	for _, statement := range []string{
		"BEGIN",
		// The restored row carries this transaction's id from an older
		// cluster, stamped a day earlier.
		`UPDATE config_artifact_content_versions
SET bumped_xid = pg_current_xact_id(), bumped_at = now() - interval '1 day'
WHERE repo_id = 'repo-restored'`,
		insertSQL("repo-restored", "p/1"),
		insertSQL("repo-restored", "p/2"),
		"COMMIT",
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%.60q: %v", statement, err)
		}
	}
	if got := versionOf(ctx, t, db, "repo-restored"); got != 1 {
		t.Fatalf("version after a write whose xid a restored row holds = %d, want 1", got)
	}
}
