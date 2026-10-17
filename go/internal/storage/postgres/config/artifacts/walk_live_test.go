// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7126 disposable PostgreSQL 18 proof of the config-artifact walker cursor claim: consecutive pages, wrap at a short page and at an exact page boundary, and disjoint pages for five concurrent claimers; enrolled in the live-postgres-readiness runner; nine proofs took 0.88-1.15s each and 9.1s for the package (disposable database plus full bootstrap per test) on a local PostgreSQL 18.6 on 2026-10-11; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// seedVersions writes one version row per repository, as the PR3b-1b seed
// will, so the walker's universe exists without a content_files trigger.
func seedVersions(ctx context.Context, t *testing.T, db *sql.DB, versions map[string]int64) {
	t.Helper()
	for repoID, version := range versions {
		if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_content_versions (repo_id, version, bumped_xid, bumped_at)
VALUES ($1, $2, pg_current_xact_id(), now())`, repoID, version); err != nil {
			t.Fatalf("seed version row %s: %v", repoID, err)
		}
	}
}

func repoNames(n int) map[string]int64 {
	out := make(map[string]int64, n)
	for i := range n {
		out[fmt.Sprintf("repo-%02d", i)] = 0
	}
	return out
}

// TestClaimPageWalksEveryRepositoryAndWrapsLive walks ten repositories in
// pages of four: the pages are consecutive in repo_id order, the stored cursor
// follows each page, and the short last page resets the cursor so the next
// claim starts the walk over.
func TestClaimPageWalksEveryRepositoryAndWrapsLive(t *testing.T) {
	ctx, db := openDatabase(t)
	database := postgres.SQLDB{DB: db}
	seedVersions(ctx, t, db, repoNames(10))

	if cursor, err := artifacts.LoadCursor(ctx, database); err != nil || cursor != "" {
		t.Fatalf("LoadCursor() before any claim = %q, %v; want empty", cursor, err)
	}
	wantPages := [][]string{
		{"repo-00", "repo-01", "repo-02", "repo-03"},
		{"repo-04", "repo-05", "repo-06", "repo-07"},
		{"repo-08", "repo-09"},
		{"repo-00", "repo-01", "repo-02", "repo-03"},
	}
	wantNext := []string{"repo-03", "repo-07", "", "repo-03"}
	for i, want := range wantPages {
		page, next, err := artifacts.ClaimPage(ctx, database, 4)
		if err != nil {
			t.Fatalf("ClaimPage() #%d error = %v", i, err)
		}
		if !reflect.DeepEqual(page, want) || next != wantNext[i] {
			t.Fatalf("ClaimPage() #%d = %v, next %q; want %v, next %q", i, page, next, want, wantNext[i])
		}
		stored, err := artifacts.LoadCursor(ctx, database)
		if err != nil || stored != wantNext[i] {
			t.Fatalf("LoadCursor() after claim #%d = %q, %v; want %q", i, stored, err, wantNext[i])
		}
	}
}

// TestClaimPageAtAnExactPageBoundaryLive covers a universe that is a whole
// number of pages: the last full page keeps a cursor, and the next claim is
// empty and resets it.
func TestClaimPageAtAnExactPageBoundaryLive(t *testing.T) {
	ctx, db := openDatabase(t)
	database := postgres.SQLDB{DB: db}
	seedVersions(ctx, t, db, repoNames(4))

	for i, wantLen := range []int{2, 2, 0, 2} {
		page, _, err := artifacts.ClaimPage(ctx, database, 2)
		if err != nil {
			t.Fatalf("ClaimPage() #%d error = %v", i, err)
		}
		if len(page) != wantLen {
			t.Fatalf("ClaimPage() #%d = %v, want %d repositories", i, page, wantLen)
		}
	}
	if _, _, err := artifacts.ClaimPage(ctx, database, 0); err == nil {
		t.Fatal("ClaimPage(budget 0) error = nil, want a refusal")
	}
}

// TestClaimPageGivesConcurrentClaimersDisjointPagesLive runs five claims at
// once over ten repositories in pages of two. The cursor row lock serializes
// them, so together they cover every repository exactly once.
func TestClaimPageGivesConcurrentClaimersDisjointPagesLive(t *testing.T) {
	ctx, db := openDatabase(t)
	database := postgres.SQLDB{DB: db}
	repos := repoNames(10)
	seedVersions(ctx, t, db, repos)

	var (
		mu      sync.Mutex
		claimed []string
		wg      sync.WaitGroup
		errs    = make(chan error, 5)
	)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, _, err := artifacts.ClaimPage(ctx, database, 2)
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			claimed = append(claimed, page...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ClaimPage() error = %v", err)
	}
	sort.Strings(claimed)
	want := make([]string, 0, len(repos))
	for repo := range repos {
		want = append(want, repo)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(claimed, want) {
		t.Fatalf("concurrent claims covered %v, want each of %v exactly once", claimed, want)
	}
}
