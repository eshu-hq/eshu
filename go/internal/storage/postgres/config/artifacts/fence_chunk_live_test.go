// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 ruling arb-8155-f1 2.7b: a real PruneSupersededGenerations chunk cycle (over-limit target page drained, normal batch non-empty, one repository) parked between its two content_files DELETEs and raced with ContentWriter.Write tombstoning and upserting doomed keys, 20 rounds, 0 deadlocks and exactly 3 bumps per round; plus the SQL two-statement transaction against an autocommit DELETE, 10 rounds; both deadlock on the immediate fence (RED on the commit that adds this file: 20 of 20 and round 0); enrolled in the live-postgres-readiness runner; about 23 s and 2 s on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Ruling arb-8155-f1 (review finding F1): a retention chunk cycle deletes
// content_files twice in one transaction, first the over-limit target's page
// (generationRetentionChunkPruneFilesQuery), then the normal batch
// (pruneContentFilesForGenerationsQuery). A fence that bumps the version row
// inside the first DELETE holds it until commit; a content Write that locks a
// doomed row of the normal batch and then waits on that version row closes a
// cycle with the second DELETE. The commit-time fence takes version rows only
// inside COMMIT, after every content lock, so no cycle exists.

// waitLockedOrDone returns when done closes or a backend whose query holds
// fragment, and not exclude, waits on a heavyweight lock, whichever comes
// first.
func waitLockedOrDone(ctx context.Context, t *testing.T, db *sql.DB, fragment, exclude string, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'
  AND pid <> pg_backend_pid() AND strpos(query, $1) > 0
  AND ($2 = '' OR strpos(query, $2) = 0)`, fragment, exclude).Scan(&waiting); err != nil {
			t.Fatalf("poll lock waiters: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("neither done nor waiting on %q", fragment)
}

// chunkRoundFixture seeds one repository with an over-limit superseded
// generation (80 file facts, BatchRowLimit 50, so the pass drains one 50-fact
// page) and a normal superseded generation of another scope on the same
// repository (10 file facts), plus one planted reference row on a normal key
// that the test holds to park retention between its two content DELETEs.
func chunkRoundFixture(ctx context.Context, t *testing.T, db *sql.DB, round int) (repo, holdPath string) {
	t.Helper()
	repo = fmt.Sprintf("repo-chunk-%02d", round)
	seedZeroVersions(ctx, t, db, repo)
	seedRetentionScopes(ctx, t, db, []retentionScope{
		{fmt.Sprintf("sc-target-%02d", round), repo, "t", 80},
		{fmt.Sprintf("sc-normal-%02d", round), repo, "n", 10},
	})
	holdPath = "n/f_9.go"
	mustExec(ctx, t, db, fmt.Sprintf(`
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
VALUES ('%s', '%s', 'zz_hold', 'x', now())`, repo, holdPath))
	return repo, holdPath
}

// TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive is ruling 2.7b(i):
// a real PruneSupersededGenerations chunk cycle (target page drained, normal
// batch non-empty, one repository) raced with ContentWriter.Write tombstoning
// and upserting doomed keys of the normal batch, 20 rounds. Retention is
// parked on a held reference row of the normal batch after its first content
// DELETE; the Write runs; then retention resumes into its second content
// DELETE. 0 deadlocks; the repository gains exactly 3 bumps per round (one
// for the retention transaction, one per Write transaction: a tombstone chunk
// and an upsert batch).
func TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive(t *testing.T) {
	ctx, db := openDatabase(t)
	policy := retentionPolicy()
	policy.BatchRowLimit = 50
	deadlocks := 0
	for round := range 20 {
		repo, holdPath := chunkRoundFixture(ctx, t, db, round)
		before := versionOf(ctx, t, db, repo)
		holder := holdRows(ctx, t, db, `
SELECT 1 FROM content_file_references WHERE repo_id = $1 AND relative_path = $2 FOR UPDATE`, repo, holdPath)

		var wg sync.WaitGroup
		var pruneErr error
		var result postgres.GenerationRetentionResult
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, pruneErr = postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: db}).
				PruneSupersededGenerations(ctx, policy)
		}()
		waitForLockWaiters(ctx, t, db, "DELETE FROM content_file_references AS t", 1)

		mat := content.Materialization{RepoID: repo, ScopeID: "scope-writer", GenerationID: "gen-writer"}
		mat.Records = []content.Record{
			{Path: "n/f_1.go", Body: "rewritten"},
			{Path: "n/f_2.go", Deleted: true},
		}
		writeDone := make(chan struct{})
		var writeErr error
		go func() {
			defer close(writeDone)
			_, writeErr = newWriter(db).Write(ctx, mat)
		}()
		// The Write's statements name content_files; retention, parked on
		// the held reference row, names content_file_references.
		waitLockedOrDone(ctx, t, db, "content_files", "content_file_references", writeDone)
		holder.release(t)
		wg.Wait()
		<-writeDone

		for name, err := range map[string]error{"prune": pruneErr, "write": writeErr} {
			switch {
			case err == nil:
			case isDeadlock(err):
				deadlocks++
				t.Logf("round %d: %s lost a deadlock: %v", round, name, err)
			default:
				t.Fatalf("round %d %s: %v", round, name, err)
			}
		}
		if pruneErr == nil && writeErr == nil {
			if got := versionOf(ctx, t, db, repo) - before; got != 3 {
				t.Errorf("round %d: %s bumped %d times, want 3", round, repo, got)
			}
			if result.GenerationsPruned != 1 {
				t.Errorf("round %d: pruned %d generations, want the normal one only (the target is chunked)", round, result.GenerationsPruned)
			}
		}
	}
	if deadlocks != 0 {
		t.Fatalf("%d deadlocks in 20 chunk-cycle rounds, want 0", deadlocks)
	}
}

// TestVersionFenceTwoStatementTransactionRacingADeleteLive is ruling
// 2.7b(ii), the SQL-level shape of F1: transaction R deletes (A, a1), then
// (A, a2) and (A, a3) in a second statement; an autocommit W deletes (A, a3)
// and (A, a2) in between. With a bump inside the first statement W waits on
// R's version row while holding a2 and a3, and R's second statement waits on
// W: a deadlock. With the commit-time bump W commits first; 0 deadlocks over
// 10 rounds and the version moves by exactly 2 per round.
func TestVersionFenceTwoStatementTransactionRacingADeleteLive(t *testing.T) {
	ctx, db := openDatabase(t)
	for round := range 10 {
		repo := fmt.Sprintf("repo-two-%02d", round)
		seedZeroVersions(ctx, t, db, repo)
		insertContentRows(ctx, t, db, "a", 3, repo)
		before := versionOf(ctx, t, db, repo)

		r, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.ExecContext(ctx, `DELETE FROM content_files WHERE repo_id = $1 AND relative_path = 'a/f_1.go'`, repo); err != nil {
			t.Fatal(err)
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		mustConnExec(ctx, t, conn, "SET enable_hashjoin = off; SET enable_mergejoin = off; SET enable_seqscan = off")
		wDone := make(chan struct{})
		var wErr error
		go func() {
			defer close(wDone)
			_, wErr = conn.ExecContext(ctx, `/* fence-two-w */ DELETE FROM content_files AS c
USING (VALUES ($1, 'a/f_3.go'), ($1, 'a/f_2.go')) AS v(r, p)
WHERE c.repo_id = v.r AND c.relative_path = v.p`, repo)
		}()
		waitLockedOrDone(ctx, t, db, "fence-two-w", "", wDone)
		_, rErr := r.ExecContext(ctx, `DELETE FROM content_files WHERE repo_id = $1 AND relative_path IN ('a/f_2.go', 'a/f_3.go')`, repo)
		if rErr == nil {
			rErr = r.Commit()
		} else {
			_ = r.Rollback()
		}
		<-wDone
		_ = conn.Close()
		for name, err := range map[string]error{"R": rErr, "W": wErr} {
			if isDeadlock(err) {
				t.Fatalf("round %d: %s lost a deadlock: %v", round, name, err)
			}
			if err != nil {
				t.Fatalf("round %d %s: %v", round, name, err)
			}
		}
		if got := versionOf(ctx, t, db, repo) - before; got != 2 {
			t.Fatalf("round %d: version moved %d, want 2 (one per transaction)", round, got)
		}
	}
}

// mustConnExec runs statement on one pinned connection.
func mustConnExec(ctx context.Context, t *testing.T, conn *sql.Conn, statement string) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, statement); err != nil {
		t.Fatalf("exec %.80q: %v", statement, err)
	}
}
