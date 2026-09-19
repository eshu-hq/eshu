// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestBackfillLiveClearsEveryFenceMarkOnAFreshInstall proves a fresh install
// whose content was seeded without the writer setting (an older binary, or a
// fixture loader) reaches a ready read model from the backfill alone, with no
// reducer: every seeded repository is marked, and the backfill's
// whole-repository derive discharges each mark, including a repository whose
// infra rows were all deleted again, which has no content or table rows left
// to list it by.
func TestBackfillLiveClearsEveryFenceMarkOnAFreshInstall(t *testing.T) {
	awareDB, ctx := isolatedDB(t)
	database := postgres.SQLDB{DB: awareDB}
	legacyDB := isolatedPlainDB(t, awareDB)

	putContent(t, ctx, legacyDB, "repo-seeded", contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	putContent(t, ctx, legacyDB, "repo-emptied", contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	if _, err := legacyDB.ExecContext(ctx, `DELETE FROM content_entities WHERE repo_id = 'repo-emptied'`); err != nil {
		t.Fatalf("empty repo: %v", err)
	}
	state, err := inventory.ReadFenceState(ctx, database, time.Now())
	if err != nil || state.DirtyRepos != 2 {
		t.Fatalf("seeded fence state = %+v, %v; want 2 marks", state, err)
	}

	if _, err := (inventory.Backfiller{DB: database}).Run(ctx); err != nil {
		t.Fatalf("Backfiller.Run() error = %v", err)
	}
	state, err = inventory.ReadFenceState(ctx, database, time.Now())
	if err != nil {
		t.Fatalf("ReadFenceState() error = %v", err)
	}
	if state.DirtyRepos != 0 || state.ReadModelState() != inventory.StateReady {
		t.Fatalf("after backfill: %+v (%s), want no marks and ready", state, state.ReadModelState())
	}
	if got := tableRows(t, ctx, awareDB, "repo-seeded"); len(got) != 1 {
		t.Fatalf("seeded repository table rows = %v, want its one infra row", got)
	}
}
