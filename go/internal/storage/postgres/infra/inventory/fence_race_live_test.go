// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestWriterFenceLiveRepairWaitsForAnOpenUnawareWrite proves the fence's
// interleaving. The repository is already marked dirty. An unaware writer
// opens a transaction and writes one more infra row; its trigger upserts the
// existing mark, which locks it. A fence repair started now must wait on that
// lock rather than clear the mark and re-derive without the row. Once the
// writer commits, the repair re-derives the row and clears the mark. With a
// trigger that only did ON CONFLICT DO NOTHING, the repair would not wait,
// the row would be missing from the table, and no mark would remain.
func TestWriterFenceLiveRepairWaitsForAnOpenUnawareWrite(t *testing.T) {
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
	if !isDirty(t, ctx, awareDB, repo) {
		t.Fatal("setup: the committed unaware write did not mark the repository")
	}

	writer, err := legacyDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin unaware writer: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, 'c.tf', 'TerraformResource', 'r.c', 1, 2, '', '{}'::jsonb, now())`,
		repo+"/c", repo); err != nil {
		t.Fatalf("open unaware write: %v", err)
	}

	done := make(chan inventory.ReconcileBatch, 1)
	failed := make(chan error, 1)
	go func() {
		batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 1000})
		if err != nil {
			failed <- err
			return
		}
		done <- batch
	}()

	// The repair must block on the mark's row lock while the write is open.
	waiting := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case <-done:
			t.Fatal("the repair finished while an unaware write that marks the repository was still open")
		case err := <-failed:
			t.Fatalf("ReconcileCycle() error = %v", err)
		default:
		}
		if err := awareDB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE wait_event_type = 'Lock'
      AND query LIKE '%DELETE FROM infra_resource_entity_dirty_repos%'
)`).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("the repair never waited on the open unaware write's mark")
	}

	if err := writer.Commit(); err != nil {
		t.Fatalf("commit unaware writer: %v", err)
	}
	select {
	case err := <-failed:
		t.Fatalf("ReconcileCycle() error = %v", err)
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the repair did not finish after the unaware write committed")
	}
	if got := tableRows(t, ctx, awareDB, repo); len(got) != 3 {
		t.Fatalf("table rows after repair = %v, want a, b, and the concurrently written c", got)
	}
	if isDirty(t, ctx, awareDB, repo) {
		t.Fatal("the repair left the mark behind after re-deriving every committed row")
	}
}
