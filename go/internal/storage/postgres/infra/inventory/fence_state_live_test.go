// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestReadFenceStateLiveReportsMarksAndReadiness proves the one query behind
// the fence gauges and the admin status field: it reports the marker, the
// number of marked repositories, the oldest mark's age, and the resulting read
// model state, and a reconcile cycle carries the same counts before it drains
// them.
func TestReadFenceStateLiveReportsMarksAndReadiness(t *testing.T) {
	sqlDB, ctx := isolatedDB(t)
	database := postgres.SQLDB{DB: sqlDB}

	state, err := inventory.ReadFenceState(ctx, database, time.Now())
	if err != nil {
		t.Fatalf("ReadFenceState() error = %v", err)
	}
	if !state.Installed || state.MarkerPresent || state.DirtyRepos != 0 || state.ReadModelState() != "backfilling" {
		t.Fatalf("empty state = %+v (%s), want installed, no marker, no marks, backfilling", state, state.ReadModelState())
	}

	recordMarker(t, ctx, database)
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO infra_resource_entity_dirty_repos (repo_id, marked_at)
VALUES ('repo-old', now() - interval '2 minutes'), ('repo-new', now())`); err != nil {
		t.Fatalf("seed marks: %v", err)
	}
	state, err = inventory.ReadFenceState(ctx, database, time.Now())
	if err != nil {
		t.Fatalf("ReadFenceState() error = %v", err)
	}
	if !state.MarkerPresent || state.DirtyRepos != 2 || state.ReadModelState() != "fenced" {
		t.Fatalf("marked state = %+v (%s), want marker, 2 marks, fenced", state, state.ReadModelState())
	}
	if state.OldestDirtyAge < 110*time.Second || state.OldestDirtyAge > 10*time.Minute {
		t.Fatalf("oldest mark age = %s, want about 2m", state.OldestDirtyAge)
	}

	batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 10})
	if err != nil {
		t.Fatalf("ReconcileCycle() error = %v", err)
	}
	if batch.DirtyRepos != 2 || batch.DirtyOldestAge < 110*time.Second {
		t.Fatalf("cycle fence counts = %d / %s, want 2 / about 2m", batch.DirtyRepos, batch.DirtyOldestAge)
	}
	state, err = inventory.ReadFenceState(ctx, database, time.Now())
	if err != nil {
		t.Fatalf("ReadFenceState() error = %v", err)
	}
	if state.DirtyRepos != 0 || state.ReadModelState() != "ready" {
		t.Fatalf("drained state = %+v (%s), want no marks, ready", state, state.ReadModelState())
	}
}
