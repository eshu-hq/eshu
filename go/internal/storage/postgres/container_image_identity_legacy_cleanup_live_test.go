// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

func TestContainerImageIdentityLegacyCleanupProbeHonorsMarkerPresentRowsLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-legacy-cleanup-probe"
		generationID = "generation:5854-legacy-cleanup-probe"
		workItemID   = "legacy-cleanup-probe-5854"
		owner        = "reducer-5854-legacy-cleanup-probe"
	)
	now := time.Date(2026, time.July, 30, 23, 30, 0, 0, time.UTC)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		workItemID,
		scopeID,
		generationID,
		owner,
		now.Add(time.Minute),
		now,
	)
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generationID)

	store := NewContainerImageIdentityCutoverStore(SQLDB{DB: db})
	complete, err := store.ContainerImageIdentityLegacyCleanupComplete(
		ctx,
		scopeID,
		generationID,
	)
	if err != nil {
		t.Fatalf("probe empty completed cutover: %v", err)
	}
	if !complete {
		t.Fatal("empty completed cutover probe = false, want true")
	}

	if _, err := db.ExecContext(ctx, `
ALTER TABLE fact_records DISABLE TRIGGER USER
`); err != nil {
		t.Fatalf("disable fact guards for backfill-shape fixture: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		containerImageIdentityAckLegacyFactInsertSQL("held-after-marker"),
		scopeID,
		generationID,
	); err != nil {
		t.Fatalf("seed marker-present held legacy row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
ALTER TABLE fact_records ENABLE TRIGGER USER
`); err != nil {
		t.Fatalf("re-enable fact guards after backfill-shape fixture: %v", err)
	}

	complete, err = store.ContainerImageIdentityLegacyCleanupComplete(
		ctx,
		scopeID,
		generationID,
	)
	if err != nil {
		t.Fatalf("probe marker-present legacy row: %v", err)
	}
	if complete {
		t.Fatal("marker-present legacy row probe = true, want false")
	}
	if _, err := db.ExecContext(ctx, `
DELETE FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = 'reducer_container_image_identity'
  AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2'
`, scopeID, generationID); err != nil {
		t.Fatalf("remove held legacy row: %v", err)
	}
	complete, err = store.ContainerImageIdentityLegacyCleanupComplete(
		ctx,
		scopeID,
		generationID,
	)
	if err != nil {
		t.Fatalf("probe cleaned marker-present row: %v", err)
	}
	if !complete {
		t.Fatal("cleaned marker-present probe = false, want true")
	}
}
