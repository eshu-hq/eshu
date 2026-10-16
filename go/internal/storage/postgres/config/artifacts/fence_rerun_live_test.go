// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"fmt"
	"testing"
)

// TestVersionFenceMigrationReRunsUnderAConcurrentWriterLive is review finding
// N1. The migrator records a receipt after the file commits, so a crash
// between the two replays migration 184 while other replicas keep writing
// through the live triggers. A content statement holds RowExclusive on
// content_files from its start and takes RowExclusive on the pending table
// only when its staging trigger fires at its end. A re-run that locks the
// pending table before it waits on content_files closes a cycle with that
// writer. The file must lock content_files first and leave an existing
// pending table alone. 10 rounds, each re-applying 184 while a writer sleeps
// mid-statement: 0 deadlocks, and the writer's commit still bumps once.
func TestVersionFenceMigrationReRunsUnderAConcurrentWriterLive(t *testing.T) {
	ctx, db := openDatabase(t)
	fence := migrationSQL(t, fenceMigrations[0])
	for round := range 10 {
		repo := fmt.Sprintf("repo-rerun-%02d", round)
		seedZeroVersions(ctx, t, db, repo)
		writerDone := make(chan error, 1)
		go func() {
			_, err := db.ExecContext(ctx, fmt.Sprintf(`/* fence-rerun-writer */
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT '%s', 'w/f_' || g, 'x', 'h', 1, now()
FROM generate_series(1, 2) AS g CROSS JOIN LATERAL (SELECT pg_sleep(0.5)) AS pause`, repo))
			writerDone <- err
		}()
		waitForSleeper(ctx, t, db, "fence-rerun-writer")
		_, migrateErr := db.ExecContext(ctx, fence)
		writerErr := <-writerDone
		for name, err := range map[string]error{"re-run": migrateErr, "writer": writerErr} {
			if isDeadlock(err) {
				t.Fatalf("round %d: the %s lost a deadlock: %v", round, name, err)
			}
			if err != nil {
				t.Fatalf("round %d %s: %v", round, name, err)
			}
		}
		if got := versionOf(ctx, t, db, repo); got != 1 {
			t.Fatalf("round %d: version %d after the writer committed, want 1", round, got)
		}
		if n := pendingRows(ctx, t, db); n != 0 {
			t.Fatalf("round %d: %d pending rows left, want 0", round, n)
		}
	}
}
