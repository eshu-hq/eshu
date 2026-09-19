// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestWriterFenceLiveTwoReplicasRepairAMarkOnce runs two reconcile replicas
// against one mark. Both list it, then block (one on the mark's row lock, the
// other on the repository lock) behind an open unaware write. After the write
// commits, the first replica clears the mark and re-derives; the second finds
// the mark already gone and must neither re-derive nor report a repair, so
// the fenced counter counts each mark once however many replicas run.
func TestWriterFenceLiveTwoReplicasRepairAMarkOnce(t *testing.T) {
	awareDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: awareDB}
	recordMarker(t, ctx, database)
	legacyDB := plainDB(t)

	repo := uniqueRepo(t)
	t.Cleanup(func() {
		_, _ = awareDB.ExecContext(context.Background(),
			`DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, repo)
	})
	seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	putContent(t, ctx, legacyDB, repo, contentRow{"b", "b.tf", "TerraformResource", "r.b", `{}`})

	writer, err := legacyDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin unaware writer: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, 'c.tf', 'TerraformResource', 'r.c', 1, 2, '', '{}'::jsonb, now())`, repo+"/c", repo); err != nil {
		t.Fatalf("open unaware write: %v", err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		fenced  int
		repairs []int64
		errs    []error
	)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 1000})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			for _, r := range batch.Repos {
				if r.RepoID == repo && r.Outcome == inventory.ReconcileFenced {
					fenced++
					repairs = append(repairs, r.Repair.Inserted)
				}
			}
		}()
	}

	// Both replicas must be waiting on a lock before the write commits.
	waiting := 0
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && waiting < 2; {
		if err := awareDB.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'
  AND (query LIKE '%infra_resource_entity_dirty_repos%' OR query LIKE '%pg_advisory_xact_lock%')
  AND query NOT LIKE '%pg_stat_activity%'`).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if waiting < 2 {
		t.Fatalf("only %d replicas waited on the open unaware write; the test did not race them", waiting)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit unaware writer: %v", err)
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("ReconcileCycle() errors = %v", errs)
	}
	if fenced != 1 {
		t.Fatalf("fenced repairs for one mark = %d (rows re-inserted %v), want exactly 1", fenced, repairs)
	}
	if got := tableRows(t, ctx, awareDB, repo); len(got) != 3 {
		t.Fatalf("table rows after repair = %v, want a, b, c", got)
	}
	if isDirty(t, ctx, awareDB, repo) {
		t.Fatal("the mark survived the repair")
	}
}

// TestWriterFenceLiveOnlyWholeRepoDerivesDischargeMarks proves a path derive
// leaves a fence mark in place and a whole-repository derive (the backfill's
// MirrorRepo) clears it.
func TestWriterFenceLiveOnlyWholeRepoDerivesDischargeMarks(t *testing.T) {
	awareDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: awareDB}
	legacyDB := plainDB(t)

	repo := uniqueRepo(t)
	t.Cleanup(func() {
		_, _ = awareDB.ExecContext(context.Background(),
			`DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, repo)
	})
	putContent(t, ctx, legacyDB, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"a.tf"}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	if !isDirty(t, ctx, awareDB, repo) {
		t.Fatal("a path derive cleared the fence mark")
	}
	if _, err := inventory.MirrorRepo(ctx, database, repo); err != nil {
		t.Fatalf("MirrorRepo() error = %v", err)
	}
	if isDirty(t, ctx, awareDB, repo) {
		t.Fatal("a whole-repository derive left the fence mark")
	}
}
