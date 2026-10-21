// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestProjectorClaimSweepsPostFenceRevivedRow is the #8695 regression pin.
// A failed projector row on a superseded generation revives to pending after
// the fence (the poison auto-retry race: its SELECT ran before the supersede
// committed, so its UPDATE re-verifies only the dead_letter status). The next
// Claim must sweep the revived row to superseded instead of claiming it, so
// no projection loop ever runs the marker for a retired generation.
func TestProjectorClaimSweepsPostFenceRevivedRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	database := openClaimDeadlockProofDB(t, dsn, 2)
	workItemID := "revived_scope-rv_scope-rv-gen-old"
	oldSupersededAt := seedSupersededClaimScope(t, database, "scope-rv",
		supersededClaimRow{workItemID: workItemID, status: "dead_letter"})

	// Post-fence revival with the poison RecoverDeadLetters SET shape (see
	// RecoverDeadLettersQuery in poison/liveness_sql.go: the UPDATE leg
	// re-verifies only the dead_letter status, so a SELECT snapshot taken
	// before the supersede commits still revives the row after it. The
	// fixture tracks that production statement; if its SET shape changes,
	// update this UPDATE to match.
	revived, err := database.ExecContext(ctx, `
UPDATE fact_work_items AS target
SET status = 'pending',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
WHERE work_item_id = $2 AND status = 'dead_letter'`, time.Now().UTC(), workItemID)
	if err != nil {
		t.Fatalf("revive dead_letter row: %v", err)
	}
	if n, _ := revived.RowsAffected(); n != 1 {
		t.Fatalf("revival updated %d rows, want 1: the race fixture did not set up", n)
	}

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if work, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("Claim() = (%s, %v, %v), want no claim of the revived retired-generation row",
			work.Generation.GenerationID, ok, err)
	}
	state := readSupersededClaimSweepState(t, database, workItemID)
	if state.status != "superseded" ||
		state.failureClass != "projector_superseded_by_newer_generation" ||
		state.generationStatus != "superseded" ||
		state.leaseOwner.Valid || state.claimUntil.Valid || state.attempts != 1 {
		t.Fatalf("revived row = %+v, want superseded, lease cleared, attempts 1, generation_status superseded", state)
	}
	var markerSet bool
	if err := database.QueryRowContext(ctx,
		`SELECT projection_write_started_at IS NOT NULL FROM scope_generations WHERE generation_id = 'scope-rv-gen-old'`).Scan(&markerSet); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if markerSet {
		t.Fatal("revived retired generation carries a write-start marker: a projection loop wrote for it")
	}
	assertPublishedGenerationUntouched(t, database, "scope-rv", oldSupersededAt)
}
