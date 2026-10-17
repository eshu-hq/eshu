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
// rows. After the release: 0 deadlocks, both passes prune their whole set,
// and every shared version row is bumped exactly twice.
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
		waitForLockWaiters(ctx, t, db, retentionCommitWaiter, 2)
		version.release(t)
		wg.Wait()

		for i, r := range results {
			if isDeadlock(r.err) {
				t.Fatalf("round %d pass %d: deadlock: %v", round, i, r.err)
			}
			if r.err != nil {
				t.Fatalf("round %d pass %d: %v", round, i, r.err)
			}
			if r.pruned != repoCount*files || r.gens != repoCount {
				t.Fatalf("round %d pass %d pruned %d content rows over %d generations, want %d over %d",
					round, i, r.pruned, r.gens, repoCount*files, repoCount)
			}
		}
		for repo, v := range versionsOf(ctx, t, db, repos) {
			if v-before[repo] != 2 {
				t.Errorf("round %d: %s bumped %d times, want 2 (once per prune)", round, repo, v-before[repo])
			}
		}
	}
}
