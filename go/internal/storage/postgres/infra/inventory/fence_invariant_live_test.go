// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// TestWriterFenceLiveInvariantUnderConcurrentWritesAndRepairs is the fence's
// concurrency proof. For several seconds an unfenced writer upserts and
// deletes infra rows of one repository, a reconcile loop drains fence marks,
// and a REPEATABLE READ reader checks, under one snapshot, that when the
// repository carries no mark its content digest equals its table digest:
// "no mark" must never coexist with a stale table. After the writer stops,
// one more cycle leaves no mark and equal digests.
func TestWriterFenceLiveInvariantUnderConcurrentWritesAndRepairs(t *testing.T) {
	awareDB, ctx := isolatedDB(t)
	database := postgres.SQLDB{DB: awareDB}
	recordMarker(t, ctx, database)
	legacyDB := isolatedPlainDB(t, awareDB)
	const repo = "repo-race"
	seedDerivedRepo(t, ctx, database, repo, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})

	runCtx, stop := context.WithTimeout(ctx, 6*time.Second)
	defer stop()
	var (
		wg         sync.WaitGroup
		writes     atomic.Int64
		repairs    atomic.Int64
		checks     atomic.Int64
		unmarked   atomic.Int64
		violations atomic.Int64
		failure    atomic.Value
	)
	fail := func(err error) {
		if runCtx.Err() == nil {
			failure.CompareAndSwap(nil, err)
		}
	}
	wg.Add(3)
	go func() { // unfenced writer
		defer wg.Done()
		for runCtx.Err() == nil {
			k := rand.IntN(20)
			var err error
			if rand.IntN(3) == 0 {
				_, err = legacyDB.ExecContext(runCtx, `DELETE FROM content_entities WHERE entity_id = $1`,
					fmt.Sprintf("%s/k%d", repo, k))
			} else {
				_, err = legacyDB.ExecContext(runCtx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, $3, 'TerraformResource', $4, 1, 2, '', '{}'::jsonb, now())
ON CONFLICT (entity_id) DO UPDATE SET entity_name = EXCLUDED.entity_name`,
					fmt.Sprintf("%s/k%d", repo, k), repo, fmt.Sprintf("k%d.tf", k), fmt.Sprintf("n%d", rand.IntN(1000)))
			}
			if err != nil {
				fail(fmt.Errorf("unfenced write: %w", err))
				return
			}
			writes.Add(1)
		}
	}()
	go func() { // reconcile loop
		defer wg.Done()
		for runCtx.Err() == nil {
			batch, err := inventory.ReconcileCycle(runCtx, database, inventory.ReconcileRequest{Budget: 10})
			if err != nil {
				fail(fmt.Errorf("reconcile: %w", err))
				return
			}
			for _, r := range batch.Repos {
				if r.Outcome == inventory.ReconcileFenced {
					repairs.Add(1)
				}
			}
		}
	}()
	go func() { // snapshot reader
		defer wg.Done()
		for runCtx.Err() == nil {
			dirty, equal, err := snapshotInvariant(runCtx, awareDB, repo)
			if err != nil {
				fail(fmt.Errorf("snapshot read: %w", err))
				return
			}
			checks.Add(1)
			if !dirty {
				unmarked.Add(1)
				if !equal {
					violations.Add(1)
				}
			}
		}
	}()
	wg.Wait()
	if err, _ := failure.Load().(error); err != nil {
		t.Fatalf("race proof worker failed: %v", err)
	}
	t.Logf("unfenced writes=%d fenced repairs=%d snapshot checks=%d (unmarked=%d)",
		writes.Load(), repairs.Load(), checks.Load(), unmarked.Load())
	if writes.Load() == 0 || repairs.Load() == 0 || checks.Load() == 0 || unmarked.Load() == 0 {
		t.Fatal("the race proof did not exercise writes, repairs, and unmarked snapshots together")
	}
	if violations.Load() != 0 {
		t.Fatalf("%d snapshots saw no mark with a stale table", violations.Load())
	}

	if _, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 10}); err != nil {
		t.Fatalf("final ReconcileCycle() error = %v", err)
	}
	dirty, equal, err := snapshotInvariant(ctx, awareDB, repo)
	if err != nil {
		t.Fatalf("final snapshot: %v", err)
	}
	if dirty || !equal {
		t.Fatalf("after the writer stopped: dirty=%v digests equal=%v, want no mark and equal", dirty, equal)
	}
}

// snapshotInvariant reads, under one REPEATABLE READ snapshot, whether the
// repository is marked and whether its content and table digests agree.
func snapshotInvariant(ctx context.Context, db *sql.DB, repo string) (dirty, equal bool, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM infra_resource_entity_dirty_repos WHERE repo_id = $1)`, repo).Scan(&dirty); err != nil {
		return false, false, err
	}
	var contentRows, tableRows int64
	var contentHash, tableHash string
	if err := tx.QueryRowContext(ctx, inventory.ReconcileDigestSQL, repo, pgarray.StringArray(inventory.Labels)).
		Scan(&contentRows, &contentHash, &tableRows, &tableHash); err != nil {
		return false, false, err
	}
	return dirty, contentRows == tableRows && contentHash == tableHash, nil
}
