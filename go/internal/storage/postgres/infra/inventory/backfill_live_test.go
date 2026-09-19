// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// resetMarker removes the backfill marker so each live backfill test starts
// from the pre-backfill state in the shared database.
func resetMarker(t *testing.T, ctx context.Context, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM infra_resource_entity_backfill_markers`); err != nil {
		t.Fatalf("reset marker: %v", err)
	}
}

func TestBackfillLiveCoversEveryRepositoryThenMarksComplete(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	resetMarker(t, ctx, sqlDB)

	withContent := uniqueRepo(t)
	staleOnly := uniqueRepo(t)
	putContent(t, ctx, sqlDB, withContent,
		contentRow{"r1", "a.tf", "TerraformResource", "r.1", `{"provider":"aws"}`},
		contentRow{"f1", "a.go", "Function", "f", `{}`},
	)
	// A repository with table rows but no content rows left (for example its
	// content was removed while the derive step did not exist yet).
	putContent(t, ctx, sqlDB, staleOnly, contentRow{"s1", "s.tf", "TerraformResource", "r.s", `{}`})
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: staleOnly}, []string{"s.tf"}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	deleteContent(t, ctx, sqlDB, staleOnly, "s1")

	complete, err := inventory.BackfillComplete(ctx, database)
	if err != nil || complete {
		t.Fatalf("BackfillComplete() before run = %v, %v; want false, nil", complete, err)
	}
	result, err := inventory.Backfiller{DB: database}.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AlreadyComplete {
		t.Fatal("Run() reported AlreadyComplete on the first run")
	}
	if got := len(tableRows(t, ctx, sqlDB, withContent)); got != 1 {
		t.Fatalf("rows for repository with content = %d, want 1", got)
	}
	if got := len(tableRows(t, ctx, sqlDB, staleOnly)); got != 0 {
		t.Fatalf("rows for repository without content = %d, want 0", got)
	}
	complete, err = inventory.BackfillComplete(ctx, database)
	if err != nil || !complete {
		t.Fatalf("BackfillComplete() after run = %v, %v; want true, nil", complete, err)
	}

	// Second run is a no-op: it must not re-derive anything.
	putContent(t, ctx, sqlDB, withContent, contentRow{"r2", "b.tf", "TerraformResource", "r.2", `{}`})
	result, err = inventory.Backfiller{DB: database}.Run(ctx)
	if err != nil || !result.AlreadyComplete {
		t.Fatalf("second Run() = %+v, %v; want AlreadyComplete", result, err)
	}
	if got := len(tableRows(t, ctx, sqlDB, withContent)); got != 1 {
		t.Fatalf("rows after no-op run = %d, want 1 (live Writes, not the backfill, derive new content)", got)
	}
	resetMarker(t, ctx, sqlDB)
}

func TestBackfillLiveFailureLeavesNoMarker(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	resetMarker(t, ctx, sqlDB)
	putContent(t, ctx, sqlDB, uniqueRepo(t), contentRow{"r1", "a.tf", "TerraformResource", "r.1", `{}`})

	failing := failingBeginDB{SQLDB: database}
	if _, err := (inventory.Backfiller{DB: failing}).Run(ctx); err == nil {
		t.Fatal("Run() error = nil, want the derive failure")
	}
	complete, err := inventory.BackfillComplete(ctx, database)
	if err != nil || complete {
		t.Fatalf("BackfillComplete() after failed run = %v, %v; want false, nil", complete, err)
	}
}

// failingBeginDB reads through the real database but cannot begin a
// transaction, so every per-repository derive fails.
type failingBeginDB struct{ postgres.SQLDB }

func (failingBeginDB) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("injected begin failure")
}
