// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// Arm 7 cases 2 and 3 (protocol companion section 6): a retention prune and
// a content Write of one repository. A holder keeps the repository's version
// row locked, so the prune stalls after its content DELETE with its content
// row locks held, and the Write arrives while the prune is open. Ruling
// addition 3e: the Write waits for the retention commit, inside the existing
// scope-lock-hold window, and then proceeds; nothing deadlocks.

type pruneWriteCase struct {
	repo        string
	writePaths  []string // paths the Write upserts
	wantWaitOn  string   // the lock type (and relation) the Write first waits on
	wantRows    int      // content rows of the repository after both commit
	writeWaiter string   // query fragment of the waiting Write statement
}

func runPruneWrite(ctx context.Context, t *testing.T, db *sql.DB, c pruneWriteCase) {
	t.Helper()
	seedZeroVersions(ctx, t, db, c.repo)
	seedRetentionScopes(ctx, t, db, []retentionScope{{"scope-" + c.repo, c.repo, "old", 30}})
	start := versionOf(ctx, t, db, c.repo)

	holder := holdRows(ctx, t, db, `SELECT 1 FROM config_artifact_content_versions WHERE repo_id = $1 FOR UPDATE`, c.repo)
	var wg sync.WaitGroup
	var pruneErr error
	wg.Add(1)
	go func() { defer wg.Done(); _, pruneErr = prune(ctx, db) }()
	waitForLockWaiters(ctx, t, db, "DELETE FROM content_files AS t", 1)

	mat := content.Materialization{RepoID: c.repo, ScopeID: "scope-writer", GenerationID: "gen-writer"}
	for _, p := range c.writePaths {
		mat.Records = append(mat.Records, content.Record{Path: p, Body: "written after the prune"})
	}
	write := goWrite(ctx, db, &wg, mat)
	waitForLockWaiters(ctx, t, db, c.writeWaiter, 1)

	var lockType string
	if err := db.QueryRowContext(ctx, `
SELECT l.locktype || coalesce(':' || l.relation::regclass::text, '') FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
WHERE NOT l.granted AND a.datname = current_database() AND strpos(a.query, $1) > 0
LIMIT 1`, c.writeWaiter).Scan(&lockType); err != nil {
		t.Fatalf("read the Write's lock wait: %v", err)
	}
	if lockType != c.wantWaitOn {
		t.Errorf("the Write waits on a %s lock, want %s", lockType, c.wantWaitOn)
	}
	t.Logf("%s: the Write waits on a %s lock while the prune is open", c.repo, lockType)

	holder.release(t)
	wg.Wait()
	for name, err := range map[string]error{"prune": pruneErr, "write": write.err} {
		if isDeadlock(err) {
			t.Fatalf("%s deadlocked: %v", name, err)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if got := versionOf(ctx, t, db, c.repo); got != start+2 {
		t.Fatalf("version = %d, want %d (one bump for the prune, one for the Write)", got, start+2)
	}
	var rows, written int
	if err := db.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE content = 'written after the prune')
FROM content_files WHERE repo_id = $1`, c.repo).Scan(&rows, &written); err != nil {
		t.Fatal(err)
	}
	if rows != c.wantRows || written != len(c.writePaths) {
		t.Fatalf("content rows = %d (%d written), want %d (%d written)", rows, written, c.wantRows, len(c.writePaths))
	}
}

// TestVersionFencePruneAndWriteDisjointKeysLive is case 2: the Write's keys
// are not doomed, so it waits on the version row only: a tuple lock on
// config_artifact_content_versions, queued behind the prune.
func TestVersionFencePruneAndWriteDisjointKeysLive(t *testing.T) {
	ctx, db := openDatabase(t)
	runPruneWrite(ctx, t, db, pruneWriteCase{
		repo:        "repo-case-2",
		writePaths:  []string{"new/a.tf", "new/b.tf"},
		wantWaitOn:  "tuple:config_artifact_content_versions",
		wantRows:    2,
		writeWaiter: "INSERT INTO content_files",
	})
}

// TestVersionFencePruneAndWriteSharedDoomedKeyLive is case 3 (re-ruling 3, a
// convergence check): the Write upserts a path the open prune deleted. Its
// first wait is on that content row, not on the version row; after the
// retention commit the upsert inserts the row again and bumps the version.
func TestVersionFencePruneAndWriteSharedDoomedKeyLive(t *testing.T) {
	ctx, db := openDatabase(t)
	runPruneWrite(ctx, t, db, pruneWriteCase{
		repo:        "repo-case-3",
		writePaths:  []string{"old/f_1.go"},
		wantWaitOn:  "transactionid",
		wantRows:    1,
		writeWaiter: "INSERT INTO content_files",
	})
}
