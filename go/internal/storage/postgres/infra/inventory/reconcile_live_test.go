// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// seedDerivedRepo writes content rows for repo and derives the table from them,
// so the repository starts in sync.
func seedDerivedRepo(t *testing.T, ctx context.Context, database postgres.SQLDB, repo string, rows ...contentRow) {
	t.Helper()
	putContent(t, ctx, database.DB, repo, rows...)
	if _, err := inventory.MirrorRepo(ctx, database, repo); err != nil {
		t.Fatalf("MirrorRepo(%s) error = %v", repo, err)
	}
}

func TestReconcileRepoLiveRepairsInjectedDrift(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)
	seedDerivedRepo(t, ctx, database, repo,
		contentRow{"a", "a.tf", "TerraformResource", "r.a", `{"provider":"aws"}`},
		contentRow{"b", "b.tf", "TerraformVariable", "v.b", `{}`},
	)
	want := tableRows(t, ctx, sqlDB, repo)

	// Drift a writer that does not derive would leave behind: a missing row,
	// a stale row whose content row is gone, and a stale dimension value.
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM infra_resource_entities WHERE entity_id = $1`, repo+"/a"); err != nil {
		t.Fatalf("drop row: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
VALUES ($1, $2, 'gone.tf', 'TerraformResource', 'r.gone', now())`, repo+"/gone", repo); err != nil {
		t.Fatalf("add stale row: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `UPDATE infra_resource_entities SET provider = 'stale' WHERE entity_id = $1`, repo+"/b"); err != nil {
		t.Fatalf("stale value: %v", err)
	}

	if got, err := inventory.ReconcileRepo(ctx, database, repo, false); err != nil || got.Outcome != inventory.ReconcileSuspect {
		t.Fatalf("ReconcileRepo(repair=false) = %q, %v; want %q", got.Outcome, err, inventory.ReconcileSuspect)
	}
	if rows := tableRows(t, ctx, sqlDB, repo); reflect.DeepEqual(rows, want) {
		t.Fatal("a first mismatch must not repair: the table changed")
	}

	got, err := inventory.ReconcileRepo(ctx, database, repo, true)
	if err != nil {
		t.Fatalf("ReconcileRepo() error = %v", err)
	}
	if got.Outcome != inventory.ReconcileRepaired {
		t.Fatalf("outcome = %q, want %q", got.Outcome, inventory.ReconcileRepaired)
	}
	if got.ContentRows != 2 || got.TableRows != 2 {
		t.Fatalf("rows before repair content=%d table=%d, want 2 and 2", got.ContentRows, got.TableRows)
	}
	if rows := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows after repair:\n got %q\nwant %q", rows, want)
	}

	again, err := inventory.ReconcileRepo(ctx, database, repo, true)
	if err != nil {
		t.Fatalf("second ReconcileRepo() error = %v", err)
	}
	if again.Outcome != inventory.ReconcileMatch {
		t.Fatalf("second outcome = %q, want %q", again.Outcome, inventory.ReconcileMatch)
	}
}

func TestReconcileRepoLiveLeavesMatchingRepoUntouched(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)
	seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	var before time.Time
	if err := sqlDB.QueryRowContext(ctx, `SELECT updated_at FROM infra_resource_entities WHERE entity_id = $1`, repo+"/a").Scan(&before); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}

	got, err := inventory.ReconcileRepo(ctx, database, repo, true)
	if err != nil {
		t.Fatalf("ReconcileRepo() error = %v", err)
	}
	if got.Outcome != inventory.ReconcileMatch {
		t.Fatalf("outcome = %q, want %q", got.Outcome, inventory.ReconcileMatch)
	}
	var after time.Time
	if err := sqlDB.QueryRowContext(ctx, `SELECT updated_at FROM infra_resource_entities WHERE entity_id = $1`, repo+"/a").Scan(&after); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("updated_at moved %s -> %s: a matching repository must not be rewritten", before, after)
	}
}

// recordMarker lets ReconcileCycle run: the marker is global and idempotent.
func recordMarker(t *testing.T, ctx context.Context, database postgres.SQLDB) {
	t.Helper()
	if _, err := database.DB.ExecContext(ctx, `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, now()) ON CONFLICT (marker_name) DO NOTHING`, inventory.BackfillMarker); err != nil {
		t.Fatalf("record marker: %v", err)
	}
}

// TestReconcileCycleLiveBudgetBoundsEachCycle walks repositories keyset-ordered
// after a cursor and stops at the budget. A drifted repository is only a
// suspect on the walk that finds it; the next cycle re-checks it first and
// repairs it, and those re-checks count against the same budget. The
// table-only repository (every content row gone, table rows left behind) must
// be listed too.
func TestReconcileCycleLiveBudgetBoundsEachCycle(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	recordMarker(t, ctx, database)
	prefix := fmt.Sprintf("repo-6793-budget-%d", time.Now().UnixNano())
	repos := []string{prefix + "-1", prefix + "-2", prefix + "-3", prefix + "-4"}
	for _, repo := range repos {
		seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	}
	deleteContent(t, ctx, sqlDB, repos[0], "a") // table-only: stale row, no content

	first, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Cursor: prefix, Budget: 2})
	if err != nil {
		t.Fatalf("ReconcileCycle() error = %v", err)
	}
	if got := repoIDs(first.Repos); !reflect.DeepEqual(got, repos[:2]) {
		t.Fatalf("first cycle repos = %q, want %q", got, repos[:2])
	}
	if first.NextCursor != repos[1] {
		t.Fatalf("NextCursor = %q, want %q", first.NextCursor, repos[1])
	}
	if first.Repos[0].Outcome != inventory.ReconcileSuspect || first.Repos[1].Outcome != inventory.ReconcileMatch {
		t.Fatalf("outcomes = %q, %q; want suspect, match", first.Repos[0].Outcome, first.Repos[1].Outcome)
	}
	if rows := tableRows(t, ctx, sqlDB, repos[0]); len(rows) != 1 {
		t.Fatalf("suspect repo rows = %q, want the stale row still there", rows)
	}

	second, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{
		Cursor: first.NextCursor, Budget: 2, Suspects: []string{repos[0]},
	})
	if err != nil {
		t.Fatalf("second ReconcileCycle() error = %v", err)
	}
	if got := repoIDs(second.Repos); !reflect.DeepEqual(got, []string{repos[0], repos[2]}) {
		t.Fatalf("second cycle repos = %q, want the suspect then %q", got, repos[2])
	}
	if second.Repos[0].Outcome != inventory.ReconcileRepaired {
		t.Fatalf("suspect outcome = %q, want %q", second.Repos[0].Outcome, inventory.ReconcileRepaired)
	}
	if second.NextCursor != repos[2] {
		t.Fatalf("NextCursor = %q, want %q", second.NextCursor, repos[2])
	}
	if rows := tableRows(t, ctx, sqlDB, repos[0]); len(rows) != 0 {
		t.Fatalf("table-only repo rows after repair = %q, want none", rows)
	}
}

// TestReconcileCycleLiveDoesNotCountAnInFlightWriteAsRepaired reproduces a
// ContentWriter.Write caught between its content commit and its derive: the
// content rows are ahead of the table. The reconcile must not call that drift.
// It is a suspect on the first check, and once the Write's derive has run the
// next cycle's re-check matches.
func TestReconcileCycleLiveDoesNotCountAnInFlightWriteAsRepaired(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	recordMarker(t, ctx, database)
	prefix := fmt.Sprintf("repo-6793-inflight-%d", time.Now().UnixNano())
	repo := prefix + "-1"
	seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})

	// The Write commits its content rows; its derive has not run yet.
	putContent(t, ctx, sqlDB, repo, contentRow{"b", "b.tf", "TerraformResource", "r.b", `{}`})
	first, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Cursor: prefix, Budget: 1})
	if err != nil {
		t.Fatalf("ReconcileCycle() error = %v", err)
	}
	if len(first.Repos) != 1 || first.Repos[0].Outcome != inventory.ReconcileSuspect {
		t.Fatalf("first cycle = %+v, want %s suspect", first.Repos, repo)
	}

	// The Write's derive completes.
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"b.tf"}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	second, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{
		Cursor: first.NextCursor, Budget: 1, Suspects: []string{repo},
	})
	if err != nil {
		t.Fatalf("second ReconcileCycle() error = %v", err)
	}
	if len(second.Repos) != 1 || second.Repos[0].RepoID != repo || second.Repos[0].Outcome != inventory.ReconcileMatch {
		t.Fatalf("second cycle = %+v, want %s match and nothing repaired", second.Repos, repo)
	}
}

// TestReconcileStartCursorLivePicksAListedRepository proves the random start
// lands on a real repository: the pick receives the number of listed
// repositories and its index selects one of them.
func TestReconcileStartCursorLivePicksAListedRepository(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	seedDerivedRepo(t, ctx, database, uniqueRepo(t), contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	var wantLast string
	if err := sqlDB.QueryRowContext(ctx, `
SELECT max(repo_id) FROM (SELECT repo_id FROM content_entities UNION SELECT repo_id FROM infra_resource_entities) r`).Scan(&wantLast); err != nil {
		t.Fatalf("max repo: %v", err)
	}
	var sawN int
	got, err := inventory.StartCursor(ctx, database, func(n int) int { sawN = n; return n - 1 })
	if err != nil {
		t.Fatalf("StartCursor() error = %v", err)
	}
	if sawN < 1 || got != wantLast {
		t.Fatalf("StartCursor() = %q (n=%d), want the last listed repository %q", got, sawN, wantLast)
	}
}

// TestReconcileRepoLiveConcurrentDeriveDoesNotDeadlock races the reconcile of
// a drifted repository against derives of the same repository and against a
// transaction that holds the repository lock. Both take one repository lock
// per transaction, so they serialize and finish; the table ends exact.
func TestReconcileRepoLiveConcurrentDeriveDoesNotDeadlock(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)
	seedDerivedRepo(t, ctx, database, repo,
		contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`},
		contentRow{"b", "b.tf", "TerraformResource", "r.b", `{}`},
	)
	want := tableRows(t, ctx, sqlDB, repo)

	holder, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('infra_resource_entities:' || $1, 0))`, repo); err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM infra_resource_entities WHERE entity_id = $1`, repo+"/a"); err != nil {
		t.Fatalf("drop row: %v", err)
	}

	errs := make(chan error, 11)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := inventory.ReconcileRepo(ctx, database, repo, true)
		errs <- err
	}()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"a.tf", "b.tf"})
			errs <- err
		}()
	}
	time.Sleep(200 * time.Millisecond)
	if err := holder.Commit(); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("reconcile and derives did not finish: possible deadlock")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reconcile/derive error = %v", err)
		}
	}
	if rows := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows after race:\n got %q\nwant %q", rows, want)
	}
}

func repoIDs(repos []inventory.RepoReconcile) []string {
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repo.RepoID)
	}
	return out
}

func TestReconcileCycleLiveReportsReadyOnceTheMarkerExists(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)
	seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, now()) ON CONFLICT (marker_name) DO NOTHING`, inventory.BackfillMarker); err != nil {
		t.Fatalf("record marker: %v", err)
	}

	batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 1})
	if err != nil {
		t.Fatalf("ReconcileCycle() error = %v", err)
	}
	if !batch.Ready || len(batch.Repos) != 1 {
		t.Fatalf("ReconcileCycle() = %+v, want ready with one repository", batch)
	}
}
