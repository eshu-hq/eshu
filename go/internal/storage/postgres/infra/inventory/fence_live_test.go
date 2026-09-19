// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// plainDB opens a connection without the derive-aware writer session setting,
// the way a binary from before the read model connects.
func plainDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("pgx", os.Getenv("ESHU_POSTGRES_DSN"))
	if err != nil {
		t.Fatalf("open plain db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func isDirty(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string) bool {
	t.Helper()
	var dirty bool
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM infra_resource_entity_dirty_repos WHERE repo_id = $1)`, repo).Scan(&dirty); err != nil {
		t.Fatalf("read dirty mark: %v", err)
	}
	return dirty
}

// TestWriterFenceLiveMarksOnlyUnawareInfraWrites proves the rolling-upgrade
// fence: a content_entities write of an infra-typed row from a connection
// without the writer session setting (an older ingester, projector, or
// bootstrap-index binary, or manual SQL) marks the repository dirty in the
// same statement, while a derive-aware writer's write and a non-infra write
// do not. Readers stay on the graph while any repository is dirty, and the
// reconcile repairs and clears a dirty repository on its next cycle.
func TestWriterFenceLiveMarksOnlyUnawareInfraWrites(t *testing.T) {
	awareDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: awareDB}
	recordMarker(t, ctx, database)
	legacyDB := plainDB(t)

	aware := uniqueRepo(t)
	seedDerivedRepo(t, ctx, database, aware, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	if isDirty(t, ctx, awareDB, aware) {
		t.Fatal("a derive-aware writer's infra write marked the repository dirty")
	}

	nonInfra := uniqueRepo(t)
	putContent(t, ctx, legacyDB, nonInfra, contentRow{"f", "main.go", "Function", "main", `{}`})
	if isDirty(t, ctx, awareDB, nonInfra) {
		t.Fatal("an unaware writer's non-infra write marked the repository dirty")
	}

	legacy := uniqueRepo(t)
	// Live tests share one database; leave no fence mark for later cycles.
	t.Cleanup(func() {
		_, _ = awareDB.ExecContext(context.Background(),
			`DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, legacy)
	})
	seedDerivedRepo(t, ctx, database, legacy, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	putContent(t, ctx, legacyDB, legacy, contentRow{"b", "b.tf", "TerraformResource", "r.b", `{}`})
	if !isDirty(t, ctx, awareDB, legacy) {
		t.Fatal("an unaware writer's infra insert did not mark the repository dirty")
	}
	reader := inventory.Reader{DB: database}
	if ready, err := reader.Ready(ctx); err != nil || ready {
		t.Fatalf("Reader.Ready() with a dirty repository = %v, %v; want false, nil", ready, err)
	}

	batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 1000})
	if err != nil {
		t.Fatalf("ReconcileCycle() error = %v", err)
	}
	repaired := false
	for _, repo := range batch.Repos {
		if repo.RepoID == legacy && repo.Outcome == inventory.ReconcileFenced {
			repaired = true
		}
	}
	if !repaired {
		t.Fatalf("ReconcileCycle() did not repair dirty repository %s on its first cycle: %+v", legacy, batch.Repos)
	}
	if isDirty(t, ctx, awareDB, legacy) {
		t.Fatal("the repair left the dirty mark behind")
	}
	if got := tableRows(t, ctx, awareDB, legacy); len(got) != 2 {
		t.Fatalf("table rows after repair = %v, want both infra rows", got)
	}

	// Deletes and type changes from an unaware writer are drift too.
	if _, err := legacyDB.ExecContext(ctx, `DELETE FROM content_entities WHERE repo_id = $1 AND entity_id = $2`,
		legacy, legacy+"/a"); err != nil {
		t.Fatalf("legacy delete: %v", err)
	}
	if !isDirty(t, ctx, awareDB, legacy) {
		t.Fatal("an unaware writer's infra delete did not mark the repository dirty")
	}
}
