// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

func TestPostgresContainerImageIdentityHeldCanonicalStillCreatesCutover(t *testing.T) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	write := containerImageIdentityAtomicLiveWrite(
		"held-canonical",
		1,
		time.Date(2026, time.July, 30, 21, 2, 0, 0, time.UTC),
	)
	heldLegacyID := write.LegacyFactIDs[0]
	write.LegacyFactIDs = nil
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
		if _, err := db.ExecContext(
			context.Background(),
			`DELETE FROM fact_records WHERE fact_id = $1`,
			heldLegacyID,
		); err != nil {
			t.Errorf("clean held legacy row: %v", err)
		}
	})

	if err := factwrite.BatchInsertFacts(
		ctx,
		db,
		[]factwrite.Row{containerImageIdentityLegacyLiveRow(heldLegacyID, 1, false)},
	); err != nil {
		t.Fatalf("seed held legacy row: %v", err)
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("publish held canonical v2 row: %v", err)
	}

	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM container_image_identity_cutovers
		  WHERE scope_id = $1 AND generation_id = $2`,
		1, write.ScopeID, write.GenerationID,
	)
	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		1, containerImageIdentityFactID(write, write.Decisions[0]),
	)
	assertContainerImageIdentityAtomicLiveCount(
		t, ctx, db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		1, heldLegacyID,
	)

	lateLegacyID := heldLegacyID + "-late"
	assertContainerImageIdentityLegacyStatementRejected(
		t,
		factwrite.BatchInsertFacts(
			ctx,
			db,
			[]factwrite.Row{containerImageIdentityLegacyLiveRow(lateLegacyID, 2, false)},
		),
	)

	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, write.IntentID); err != nil {
		t.Fatalf("advance held-cutover claim epoch: %v", err)
	}
	stale := write
	stale.EvidenceAsOf = write.EvidenceAsOf.Add(time.Second)
	staleWriter := PostgresContainerImageIdentityWriter{
		DB:            db,
		Beginner:      &containerImageIdentityAtomicLiveBeginner{db: db},
		CutoverLookup: containerImageIdentityAtomicLiveCutoverLookup{db: db},
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
	}
	_, err := staleWriter.WriteContainerImageIdentityDecisions(ctx, stale)
	if !errors.Is(err, ErrContainerImageIdentityClaimRejected) {
		t.Fatalf("stale held-cutover write error = %v, want claim rejection", err)
	}
	assertContainerImageIdentityLiveRow(
		t,
		ctx,
		db,
		containerImageIdentityFactID(write, write.Decisions[0]),
		false,
		write.EvidenceAsOf.UnixMicro(),
	)
}
