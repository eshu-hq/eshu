// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 seed timing proof: migrations 186 and 187 on a content_files fixture over 3,000 repositories with every index present, then exactly one version-0 row per repository; 30,000 rows by default (ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS=1000000 for the ruling size: 186 0.0033 s, 187 0.1329 s, fixture load 74 s, load1 45 on 2026-10-16); enrolled in the live-postgres-readiness runner; SEED_TIMING wall is logged, never gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestVersionFenceSeedTimingLive times migrations 186 and 187 on the
// migration 169 fixture shape: 1,000,000 content_files rows over 3,000
// repositories with every content_files index and trigger present. The
// ruling bar is at most 1 s of content-write stall; this test logs the
// timings and does not gate them (shared hosts vary). It runs at 30,000
// rows by default; set ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS=1000000 for the
// ruling's fixture size.
func TestVersionFenceSeedTimingLive(t *testing.T) {
	rowsWanted := 30000
	if raw := os.Getenv("ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			t.Fatalf("ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS = %q, want a positive row count", raw)
		}
		rowsWanted = n
	}
	ctx, db := openDatabase(t)
	rewindToPreFence(ctx, t, db)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seedStart := time.Now()
	// Replica role skips the migration 138 per-row derivation while the
	// fixture loads; every index is maintained.
	for _, statement := range []string{
		"SET session_replication_role = replica",
		fmt.Sprintf(`
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-' || lpad((g %% 3000)::text, 4, '0'), 'src/f_' || g || '.go',
       'package main // ' || g, md5(g::text), 1, 'go', now()
FROM generate_series(1, %d) AS g`, rowsWanted),
		"SET session_replication_role = origin",
		"VACUUM ANALYZE content_files",
		"CHECKPOINT",
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("fixture %.60q: %v", statement, err)
		}
	}
	_ = conn.Close()
	var indexes int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename = 'content_files'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture: %d content_files rows, 3000 repositories, %d indexes, loaded in %s", rowsWanted, indexes, time.Since(seedStart))

	for _, name := range fenceMigrations {
		start := time.Now()
		mustExec(ctx, t, db, migrationSQL(t, name))
		t.Logf("SEED_TIMING migration=%s wall=%.4fs", name, time.Since(start).Seconds())
	}
	var seeded int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM config_artifact_content_versions WHERE version = 0`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != min(3000, rowsWanted) {
		t.Fatalf("seed wrote %d version-0 rows, want %d", seeded, min(3000, rowsWanted))
	}
}
