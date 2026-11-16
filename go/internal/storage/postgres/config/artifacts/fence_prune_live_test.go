// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 ruling additions 3a and 3b: auto_explain records the DELETE fence statement's plan with a Sort on repo_id under the Insert and no reordering node between; arm 7 case 1c runs two real PruneSupersededGenerations passes on scope sets sharing six repositories in opposite order, 3 rounds, 0 deadlocks, each shared version row bumped twice; needs LOAD 'auto_explain' (superuser DSN); enrolled in the live-postgres-readiness runner; 0.8-2.5 s per proof on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestVersionFenceDeletePlanSortsUnderModifyTableLive is ruling addition 3a,
// on the commit-time fence (arb-8155-f1). The ascending lock order is
// executor behavior, not a documented guarantee, so this test records the
// plan the commit statement actually runs: auto_explain logs the deferred
// trigger's upsert as a notice at COMMIT, and the Insert on
// config_artifact_content_versions must read the staged rows in repo_id
// order, either through a Sort on p.repo_id or through the pending table's
// (bumped_xid, repo_id) primary key with bumped_xid fixed, with no
// reordering node between.
func TestVersionFenceDeletePlanSortsUnderModifyTableLive(t *testing.T) {
	ctx, db := openDatabase(t)
	insertContentRows(ctx, t, db, "plan", 20, "repo-plan-a", "repo-plan-b", "repo-plan-c")

	var notices []string
	var mu sync.Mutex
	conn := noticeConn(ctx, t, db, func(_ *pgconn.PgConn, n *pgconn.Notice) {
		mu.Lock()
		notices = append(notices, n.Message)
		mu.Unlock()
	})
	for _, statement := range []string{
		"LOAD 'auto_explain'",
		"SET auto_explain.log_min_duration = 0",
		"SET auto_explain.log_nested_statements = on",
		"SET auto_explain.log_level = 'notice'",
		"DELETE FROM content_files WHERE repo_id LIKE 'repo-plan-%'",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, n := range notices {
		insertAt := strings.Index(n, "Insert on config_artifact_content_versions")
		if insertAt < 0 || !strings.Contains(n, "config_artifact_fence_pending") {
			continue
		}
		plan := n[insertAt:]
		sortAt := strings.Index(plan, "->  Sort")
		scanAt := strings.Index(plan, "Index Scan using config_artifact_fence_pending_pkey")
		switch {
		case sortAt >= 0:
			key := plan[sortAt:]
			key = key[strings.Index(key, "Sort Key:")+len("Sort Key:"):]
			key = strings.TrimSpace(key[:strings.IndexByte(key, '\n')])
			if key != "p.repo_id" {
				t.Fatalf("commit fence Sort Key = %q, want p.repo_id:\n%s", key, n)
			}
			if above := plan[:sortAt]; strings.Contains(above, "Aggregate") || strings.Contains(above, "Hash") {
				t.Fatalf("a reordering node sits between the Insert and the Sort:\n%s", n)
			}
		case scanAt >= 0:
			if above := plan[:scanAt]; strings.Contains(above, "Aggregate") || strings.Contains(above, "Hash") || strings.Contains(above, "Bitmap") {
				t.Fatalf("a reordering node sits between the Insert and the pending-key scan:\n%s", n)
			}
		default:
			t.Fatalf("commit fence plan reads the staged rows in no repo_id order:\n%s", n)
		}
		t.Logf("commit fence plan:\n%s", n)
		return
	}
	t.Fatalf("auto_explain logged no plan for the commit fence statement; notices: %q", notices)
}

// noticeConn opens a pgx connection to db's database that reports notices.
func noticeConn(ctx context.Context, t *testing.T, db *sql.DB, onNotice pgconn.NoticeHandler) *pgx.Conn {
	t.Helper()
	sqlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var config *pgx.ConnConfig
	if err := sqlConn.Raw(func(driverConn any) error {
		config = driverConn.(*stdlib.Conn).Conn().Config().Copy()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = sqlConn.Close()
	config.OnNotice = onNotice
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect notice session: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// TestVersionFenceConcurrentPrunesShareRepositoriesLive is ruling addition
// 3b, arm 7 case 1c: two real PruneSupersededGenerations passes on disjoint
// scope sets whose scopes share repository ids (two scopes, one repository,
// as migration 169 allows). Pass A prunes set X while set Y's scope rows are
// held, and stalls on a held middle version row; pass B then prunes set Y
// (X's scopes are locked by A, so B skips them) and waits on A's version
// rows. After the release: 0 deadlocks, every content row is reported once,
// the generations the passes report equal the ones gone from
// scope_generations (generations a pass drained but could not select are
// left for a later pass, and one follow-up pass deletes exactly those), and
// every shared version row is bumped exactly twice (#10770).
func TestVersionFenceConcurrentPrunesShareRepositoriesLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const repoCount, files = 6, 40
	for round := range 3 {
		var repos []string
		var setX, setY []retentionScope
		for i := range repoCount {
			repo := fmt.Sprintf("repo-1c-%d-%d", round, i)
			repos = append(repos, repo)
			setX = append(setX, retentionScope{fmt.Sprintf("sx-%d-%d", round, i), repo, "x", files})
			// Set Y lists its scopes in the opposite repository order.
			j := repoCount - 1 - i
			setY = append(setY, retentionScope{
				fmt.Sprintf("sy-%d-%d", round, j),
				fmt.Sprintf("repo-1c-%d-%d", round, j), "y", files,
			})
		}
		seedZeroVersions(ctx, t, db, repos...)
		seedRetentionScopes(ctx, t, db, append(setX, setY...))
		before := versionsOf(ctx, t, db, repos)

		version := holdRows(ctx, t, db,
			`SELECT 1 FROM config_artifact_content_versions WHERE repo_id = $1 FOR UPDATE`, repos[repoCount/2])
		ySet := holdRows(ctx, t, db,
			`SELECT 1 FROM ingestion_scopes WHERE scope_id LIKE $1 FOR UPDATE`, fmt.Sprintf("sy-%d-%%", round))
		type outcome struct {
			pruned int64
			gens   int
			err    error
		}
		results := make([]outcome, 2)
		var wg sync.WaitGroup
		run := func(i int) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := prune(ctx, db)
				results[i] = outcome{res.RowsPruned["content_files"], res.GenerationsPruned, err}
			}()
		}
		run(0)
		waitForLockWaiters(ctx, t, db, retentionCommitWaiter, 1)
		ySet.release(t)
		run(1)
		// Both passes parking on the held version row is the shape this proof
		// aims for, but a predrain batch gives up its wait after 500 ms
		// (lock_timeout), so on a loaded host the first waiter can time out
		// before the second parks. Release after a bounded wait either way:
		// every assertion below holds for any interleaving.
		if !lockWaitersReached(ctx, t, db, retentionCommitWaiter, 2, 300*time.Millisecond) {
			t.Logf("round %d: passes did not park together on the held version row; releasing it", round)
		}
		version.release(t)
		wg.Wait()

		var totalPruned int64
		var totalGens int
		for i, r := range results {
			if isDeadlock(r.err) {
				t.Fatalf("round %d pass %d: deadlock: %v", round, i, r.err)
			}
			if r.err != nil {
				t.Fatalf("round %d pass %d: %v", round, i, r.err)
			}
			totalPruned += r.pruned
			totalGens += r.gens
		}
		// The batch predrain (#10020) deletes content in committed batches
		// before Begin, so drain work migrates between the passes, and a
		// pass can end with generations it drained but did not delete.
		// Two concurrent passes both read the eligible set and, once the
		// held rows release, start their prune selections within the same
		// millisecond: the selection takes FOR UPDATE ... SKIP LOCKED on
		// every eligible scope and generation until its savepoint
		// rollback (#7334), so the pass that selects second can see an
		// empty set while the first holds the wide lock, and defers
		// generations only it drained. A batch whose commit waits on the
		// held version row longer than its 500 ms lock_timeout rolls back
		// and leaves its generation undrained the same way. Both are the
		// documented contract in generation_retention_chunk_predrain.go
		// (a later pass finishes the rest), so one pass pair is not
		// guaranteed to drain or delete all twelve generations, and the
		// old "both passes prune their whole set" expectation failed
		// about half the time (#10770). What is guaranteed, and asserted:
		//   - the generations the passes report equal the generations
		//     that are actually gone from scope_generations, so a pass
		//     can neither under-report nor over-report a deletion,
		//   - each follow-up pass deletes exactly the generations left
		//     behind, and the fixture converges with nothing superseded
		//     and no content row left,
		//   - across all passes every content row and every generation is
		//     reported exactly once,
		//   - a shared version row is bumped once per deleting
		//     transaction, never per statement or row: at least once and
		//     at most once for each of its two generations.
		want := 2 * repoCount
		left := supersededLeft(ctx, t, db, round)
		if totalGens+left != want {
			t.Fatalf("round %d passes reported %d generations pruned with %d still superseded, want %d in total: the result must account for every generation exactly once",
				round, totalGens, left, want)
		}
		for attempt := 1; left > 0; attempt++ {
			if attempt > 3 {
				t.Fatalf("round %d: %d superseded generations remain after %d follow-up passes", round, left, attempt-1)
			}
			followUp, err := prune(ctx, db)
			if err != nil {
				t.Fatalf("round %d follow-up pass %d: %v", round, attempt, err)
			}
			now := supersededLeft(ctx, t, db, round)
			if followUp.GenerationsPruned != left-now {
				t.Fatalf("round %d follow-up pass %d reported %d generations pruned but %d disappeared (%d -> %d)",
					round, attempt, followUp.GenerationsPruned, left-now, left, now)
			}
			t.Logf("round %d: follow-up pass %d deleted %d deferred generations, %d left", round, attempt, left-now, now)
			totalPruned += followUp.RowsPruned["content_files"]
			totalGens += followUp.GenerationsPruned
			left = now
		}
		if totalGens != want || totalPruned != int64(want*files) {
			t.Fatalf("round %d reported %d generations and %d content rows over all passes, want %d and %d, each exactly once",
				round, totalGens, totalPruned, want, want*files)
		}
		for _, repo := range repos {
			if n := contentCount(ctx, t, db, repo, "x") + contentCount(ctx, t, db, repo, "y"); n != 0 {
				t.Errorf("round %d: %s still holds %d content rows after every pass", round, repo, n)
			}
		}
		for repo, v := range versionsOf(ctx, t, db, repos) {
			if bumps := v - before[repo]; bumps < 1 || bumps > 2 {
				t.Errorf("round %d: %s bumped %d times, want 1 or 2 (once per generation's deleting transaction)", round, repo, bumps)
			}
		}
	}
}

// supersededLeft counts the superseded generations still present for the
// round's scopes: the fixture's end state, which the passes' returned counts
// cannot stand in for (#10770).
func supersededLeft(ctx context.Context, t *testing.T, db *sql.DB, round int) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM scope_generations
WHERE status = 'superseded' AND (scope_id LIKE $1 OR scope_id LIKE $2)`,
		fmt.Sprintf("sx-%d-%%", round), fmt.Sprintf("sy-%d-%%", round)).Scan(&n); err != nil {
		t.Fatalf("count superseded generations: %v", err)
	}
	return n
}

// lockWaitersReached polls pg_stat_activity for up to within and reports
// whether want backends waiting on a heavyweight lock ran a query containing
// fragment at the same time.
func lockWaitersReached(ctx context.Context, t *testing.T, db *sql.DB, fragment string, want int, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'
  AND pid <> pg_backend_pid() AND strpos(query, $1) > 0`, fragment).Scan(&waiting); err != nil {
			t.Fatalf("poll lock waiters: %v", err)
		}
		if waiting >= want {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
