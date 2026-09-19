// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// seedGenerationFacts records one scope, one generation, and one content_entity
// fact per entity id for repo, the shape the retention prune reads.
func seedGenerationFacts(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo, generation string, entityIDs ...string) {
	t.Helper()
	scope := "scope-" + generation
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)
ON CONFLICT (scope_id) DO NOTHING`, scope); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), 'superseded')
ON CONFLICT (generation_id) DO NOTHING`, generation, scope); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	for _, id := range entityIDs {
		if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content_entity', $1, 'git', $1, now(), now(),
    jsonb_build_object('repo_id', $4::text, 'entity_id', $5::text))`,
			generation+"/"+id, scope, generation, repo, repo+"/"+id); err != nil {
			t.Fatalf("seed fact %s: %v", id, err)
		}
	}
}

func TestRetentionLiveDeletesOnlyOrphansOfAffectedRepositories(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	affected := uniqueRepo(t)
	untouched := uniqueRepo(t)
	generation := "gen-ret-" + affected

	for _, repo := range []string{affected, untouched} {
		putContent(t, ctx, sqlDB, repo,
			contentRow{"keep", "a.tf", "TerraformResource", "r.keep", `{}`},
			contentRow{"gone", "a.tf", "TerraformResource", "r.gone", `{}`},
		)
		if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"a.tf"}); err != nil {
			t.Fatalf("MirrorPaths(%s) error = %v", repo, err)
		}
	}
	seedGenerationFacts(t, ctx, sqlDB, affected, generation, "gone")

	// Retention: lock, prune content (simulated), drop orphans, commit.
	tx, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := inventory.LockRepositoriesForGenerations(ctx, tx, []string{generation}); err != nil {
		t.Fatalf("LockRepositoriesForGenerations() error = %v", err)
	}
	for _, repo := range []string{affected, untouched} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM content_entities WHERE entity_id = $1`, repo+"/gone"); err != nil {
			t.Fatalf("prune content: %v", err)
		}
	}
	deleted, err := inventory.DeleteOrphanedRows(ctx, tx, []string{generation})
	if err != nil {
		t.Fatalf("DeleteOrphanedRows() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the affected repository's orphan)", deleted)
	}
	if got, want := len(tableRows(t, ctx, sqlDB, affected)), 1; got != want {
		t.Fatalf("affected repo rows = %d, want %d", got, want)
	}
	// The untouched repository's orphan is out of this batch's scope; its next
	// Write or the backfill converges it.
	if got, want := len(tableRows(t, ctx, sqlDB, untouched)), 2; got != want {
		t.Fatalf("untouched repo rows = %d, want %d", got, want)
	}
}

// TestRetentionLiveLocksBlockConcurrentDeriveOfSameRepository forces the
// interleaving the lock exists for: a derive of the same repository that starts
// while retention holds the lock must wait, then read the post-prune content,
// so it cannot write the pruned entity back.
func TestRetentionLiveLocksBlockConcurrentDeriveOfSameRepository(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)
	other := uniqueRepo(t)
	generation := "gen-lock-" + repo
	putContent(t, ctx, sqlDB, repo,
		contentRow{"keep", "a.tf", "TerraformResource", "r.keep", `{}`},
		contentRow{"gone", "a.tf", "TerraformResource", "r.gone", `{}`},
	)
	putContent(t, ctx, sqlDB, other, contentRow{"o1", "o.tf", "TerraformResource", "r.o1", `{}`})
	seedGenerationFacts(t, ctx, sqlDB, repo, generation, "gone")

	tx, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := inventory.LockRepositoriesForGenerations(ctx, tx, []string{generation}); err != nil {
		t.Fatalf("LockRepositoriesForGenerations() error = %v", err)
	}

	sameDone := make(chan error, 1)
	go func() {
		_, err := inventory.MirrorRepo(ctx, database, repo)
		sameDone <- err
	}()
	// A different repository is not blocked by this retention batch.
	if _, err := inventory.MirrorRepo(ctx, database, other); err != nil {
		t.Fatalf("MirrorRepo(other) error = %v, want it to proceed while %s is locked", err, repo)
	}
	select {
	case err := <-sameDone:
		t.Fatalf("MirrorRepo(same repo) finished (err=%v) while retention held its lock", err)
	case <-time.After(500 * time.Millisecond):
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM content_entities WHERE entity_id = $1`, repo+"/gone"); err != nil {
		t.Fatalf("prune content: %v", err)
	}
	if _, err := inventory.DeleteOrphanedRows(ctx, tx, []string{generation}); err != nil {
		t.Fatalf("DeleteOrphanedRows() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	select {
	case err := <-sameDone:
		if err != nil {
			t.Fatalf("MirrorRepo(same repo) error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("MirrorRepo(same repo) still blocked after retention committed")
	}
	want := []string{"keep|a.tf|TerraformResource|r.keep||||||||||"}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows after racing derive:\n got %q\nwant %q (the pruned entity must not come back)", got, want)
	}
}
