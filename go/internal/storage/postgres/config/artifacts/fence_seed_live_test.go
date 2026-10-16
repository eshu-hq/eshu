// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// fenceMigrations are the PR3b-1b migrations in apply order: the triggers,
// then the version-0 seed.
var fenceMigrations = []string{"content_files_version_fence", "config_artifact_content_versions_seed"}

func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == name {
			return def.SQL
		}
	}
	t.Fatalf("migration %s not found", name)
	return ""
}

// rewindToPreFence returns a bootstrapped database to its state before
// migration 184: no fence trigger and no version row.
func rewindToPreFence(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	dropFence(ctx, t, db)
	mustExec(ctx, t, db, `TRUNCATE config_artifact_content_versions`)
}

type versionRow struct {
	version int64
	xid     string
}

func versionRows(ctx context.Context, t *testing.T, db *sql.DB) map[string]versionRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT repo_id, version, bumped_xid::text FROM config_artifact_content_versions`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]versionRow{}
	for rows.Next() {
		var repo string
		var row versionRow
		if err := rows.Scan(&repo, &row.version, &row.xid); err != nil {
			t.Fatal(err)
		}
		out[repo] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestVersionFenceMigrationsOnExistingContentLive applies migrations 184 and
// 185 to a database that already holds content: every repository with
// content gets one version-0 row with bumped_xid 0, a version row that a
// fenced write created before the seed is kept, a re-apply of both files
// changes nothing, and the first write after the seed bumps 0 to 1 (no real
// transaction id equals 0, so the seed row never satisfies the guard).
func TestVersionFenceMigrationsOnExistingContentLive(t *testing.T) {
	ctx, db := openDatabase(t)
	rewindToPreFence(ctx, t, db)
	repos := []string{"repo-seed-a", "repo-seed-b", "repo-seed-c"}
	insertContentRows(ctx, t, db, "src", 7, repos...)
	if got := len(versionRows(ctx, t, db)); got != 0 {
		t.Fatalf("pre-fence database has %d version rows, want 0", got)
	}

	mustExec(ctx, t, db, migrationSQL(t, fenceMigrations[0]))
	insertContentRows(ctx, t, db, "late", 2, "repo-between") // a write between 184 and 185
	mustExec(ctx, t, db, migrationSQL(t, fenceMigrations[1]))

	seeded := versionRows(ctx, t, db)
	if len(seeded) != len(repos)+1 {
		t.Fatalf("seed wrote %d version rows, want %d: %v", len(seeded), len(repos)+1, seeded)
	}
	for _, repo := range repos {
		if row := seeded[repo]; row.version != 0 || row.xid != "0" {
			t.Errorf("%s seeded as version %d xid %s, want version 0 xid 0", repo, row.version, row.xid)
		}
	}
	if row := seeded["repo-between"]; row.version != 1 || row.xid == "0" {
		t.Errorf("repo-between = version %d xid %s, want the fenced write's version 1 kept", row.version, row.xid)
	}

	for _, name := range fenceMigrations {
		mustExec(ctx, t, db, migrationSQL(t, name))
	}
	if again := versionRows(ctx, t, db); fmt.Sprint(again) != fmt.Sprint(seeded) {
		t.Fatalf("re-apply changed the version rows: %v, want %v", again, seeded)
	}

	mustExec(ctx, t, db, `UPDATE content_files SET content = 'v2' WHERE repo_id = 'repo-seed-a'`)
	if got := versionOf(ctx, t, db, "repo-seed-a"); got != 1 {
		t.Fatalf("first write after the seed: version %d, want 1", got)
	}
}

// TestVersionFenceSeedRacesAFencedWriteLive runs the seed while a fenced
// write of a repository with pre-fence content and no version row has its
// version insert in flight (SET CONSTRAINTS ALL IMMEDIATE runs the
// commit-time bump early). The seed's insert waits on that uncommitted row;
// when the write commits the seed does nothing, and when it rolls back the
// seed inserts version 0. Either way: one row, no error.
func TestVersionFenceSeedRacesAFencedWriteLive(t *testing.T) {
	for _, outcome := range []string{"commit", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, db := openDatabase(t)
			rewindToPreFence(ctx, t, db)
			insertContentRows(ctx, t, db, "src", 3, "repo-race")
			mustExec(ctx, t, db, migrationSQL(t, fenceMigrations[0]))

			write, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := write.ExecContext(ctx, `UPDATE content_files SET content = 'w' WHERE repo_id = 'repo-race'`); err != nil {
				t.Fatal(err)
			}
			if _, err := write.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			seed := "/* fence-seed-race */ " + migrationSQL(t, fenceMigrations[1])
			seedDone := make(chan error, 1)
			go func() {
				_, err := db.ExecContext(ctx, seed)
				seedDone <- err
			}()
			waitForLockWaiters(ctx, t, db, "fence-seed-race", 1)
			want := int64(1)
			if outcome == "commit" {
				err = write.Commit()
			} else {
				err = write.Rollback()
				want = 0
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := <-seedDone; err != nil {
				t.Fatalf("seed: %v", err)
			}
			rows := versionRows(ctx, t, db)
			if len(rows) != 1 || rows["repo-race"].version != want {
				t.Fatalf("after the race: %v, want one repo-race row at version %d", rows, want)
			}
		})
	}
}

// TestVersionFenceSeedOrderDoesNotDeadlockWithAWriterLive is the closing
// proof of review finding F2: a writer holds the version row of a low
// repository (its bump run early by SET CONSTRAINTS ALL IMMEDIATE) and then
// commits a write of a higher one while the seed runs. The seed inserts in
// repo_id order, so it waits at the low row before it reaches the high one;
// the writer's commit takes the high row unopposed. 3 rounds over 200
// repositories: 0 deadlocks, one row per repository.
func TestVersionFenceSeedOrderDoesNotDeadlockWithAWriterLive(t *testing.T) {
	ctx, db := openDatabase(t)
	repos := make([]string, 200)
	for i := range repos {
		repos[i] = fmt.Sprintf("repo-order-%03d", i)
	}
	for round := range 3 {
		rewindToPreFence(ctx, t, db)
		mustExec(ctx, t, db, "TRUNCATE content_files CASCADE")
		insertContentRows(ctx, t, db, "src", 2, repos...)
		mustExec(ctx, t, db, migrationSQL(t, fenceMigrations[0]))
		low, high := repos[20+round], repos[180-round]

		write, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			"UPDATE content_files SET content = 'w' WHERE repo_id = '" + low + "'",
			"SET CONSTRAINTS ALL IMMEDIATE",
		} {
			if _, err := write.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		seed := "/* fence-seed-order */ " + migrationSQL(t, fenceMigrations[1])
		seedDone := make(chan error, 1)
		go func() {
			_, err := db.ExecContext(ctx, seed)
			seedDone <- err
		}()
		waitForLockWaiters(ctx, t, db, "fence-seed-order", 1)
		if _, err := write.ExecContext(ctx, "UPDATE content_files SET content = 'w' WHERE repo_id = '"+high+"'"); err != nil {
			t.Fatal(err)
		}
		writeErr := write.Commit()
		seedErr := <-seedDone
		for name, err := range map[string]error{"writer": writeErr, "seed": seedErr} {
			if isDeadlock(err) {
				t.Fatalf("round %d: %s lost a deadlock: %v", round, name, err)
			}
			if err != nil {
				t.Fatalf("round %d %s: %v", round, name, err)
			}
		}
		rows := versionRows(ctx, t, db)
		if len(rows) != len(repos) || rows[low].version != 1 || rows[high].version != 1 {
			t.Fatalf("round %d: %d rows, low %v, high %v; want %d rows, both written repositories at 1",
				round, len(rows), rows[low], rows[high], len(repos))
		}
	}
}
