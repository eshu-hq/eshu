// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestReconcileCycleLivePersistsAndResumesTheWalkCursor proves a restarted
// reducer resumes the walk where the last cycle stopped, from a one-row
// cursor, instead of enumerating every repository to choose a start. Cycle
// one resumes after a stored cursor; a second cycle with an empty in-memory
// cursor, as a fresh process has, continues after cycle one's last
// repository.
//
// It writes the shared cursor row, so it does not run in parallel with other
// persisted walks; no other live test sets Persist.
func TestReconcileCycleLivePersistsAndResumesTheWalkCursor(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	recordMarker(t, ctx, database)

	prefix := uniqueRepo(t)
	repos := make([]string, 4)
	for i := range repos {
		repos[i] = fmt.Sprintf("%s-%d", prefix, i)
		seedDerivedRepo(t, ctx, database, repos[i],
			contentRow{id: "e", path: "main.tf", entityType: "TerraformResource", name: "r"})
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO infra_resource_entity_reconcile_cursor (walk_name, cursor, updated_at)
VALUES ('infra_resource_entities', $1, now())
ON CONFLICT (walk_name) DO UPDATE SET cursor = EXCLUDED.cursor`, prefix); err != nil {
		t.Fatalf("store cursor: %v", err)
	}

	first, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 2, Persist: true})
	if err != nil {
		t.Fatalf("first ReconcileCycle() error = %v", err)
	}
	if got := repoIDs(first.Repos); !reflect.DeepEqual(got, repos[:2]) {
		t.Fatalf("first cycle checked %v, want %v (resumed after the stored cursor)", got, repos[:2])
	}
	stored, err := inventory.LoadCursor(ctx, database)
	if err != nil {
		t.Fatalf("LoadCursor() error = %v", err)
	}
	if stored != repos[1] {
		t.Fatalf("stored cursor = %q, want %q (the first cycle's last repository)", stored, repos[1])
	}

	second, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 2, Persist: true})
	if err != nil {
		t.Fatalf("second ReconcileCycle() error = %v", err)
	}
	if got := repoIDs(second.Repos); !reflect.DeepEqual(got, repos[2:]) {
		t.Fatalf("second cycle checked %v, want %v (resumed from the persisted cursor)", got, repos[2:])
	}
}
