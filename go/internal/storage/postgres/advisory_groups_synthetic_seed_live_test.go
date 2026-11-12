// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10566 disposable PostgreSQL proof that migration 295 seeds the synthetic advisory-groups anchor (synthetic kind, active, pointing at the active genesis) with no work item, and that re-applying the bootstrap converges once; seeded rows carry no work items with nothing to clean; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"testing"
	"time"
)

// Advisory-group coalescing (#10566) re-homes advisory_group_projection
// rows to a synthetic long-lived scope/generation owned by the supply-chain
// domain. Migration 295 seeds that anchor (AdvisoryGroupsSyntheticScope
// with one perpetually-active genesis generation). The seed carries no
// work item: per-group slots arise from the first touch of each group, so
// the domain row count stays bounded by the group count with no sweeper.
const advisoryGroupsSyntheticScopeKind = "synthetic"

// TestAdvisoryGroupsSyntheticSeedLive pins migration 295: every bootstrap
// carries the synthetic advisory-groups anchor (kind synthetic,
// active, pointing at the active genesis) and no work item. Re-applying
// the bootstrap converges once and is a no-op after it (every migration
// file replays on every bootstrap, so the seed must be DO NOTHING
// idempotent). Reserved-id handling is fail-safe, never overwrite: a
// colliding scope id keeps its existing row and this test fails loudly on
// the kind mismatch instead of silently adopting a foreign scope.
func TestAdvisoryGroupsSyntheticSeedLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	assertSyntheticAdvisoryAnchor := func(stage string) {
		t.Helper()
		var scopeKind, scopeStatus, activeGeneration string
		if err := db.QueryRowContext(ctx, `
SELECT scope_kind, status, active_generation_id
FROM ingestion_scopes WHERE scope_id = $1
`, AdvisoryGroupsSyntheticScope).Scan(&scopeKind, &scopeStatus, &activeGeneration); err != nil {
			t.Fatalf("%s: read synthetic advisory scope: %v", stage, err)
		}
		if scopeKind != advisoryGroupsSyntheticScopeKind || scopeStatus != "active" {
			t.Fatalf("%s: synthetic scope = kind:%q status:%q, want synthetic/active", stage, scopeKind, scopeStatus)
		}
		if activeGeneration != AdvisoryGroupsSyntheticGenesis {
			t.Fatalf("%s: synthetic active generation = %q, want %q", stage, activeGeneration, AdvisoryGroupsSyntheticGenesis)
		}
		var generationStatus string
		if err := db.QueryRowContext(ctx, `
SELECT status FROM scope_generations WHERE generation_id = $1
`, AdvisoryGroupsSyntheticGenesis).Scan(&generationStatus); err != nil {
			t.Fatalf("%s: read synthetic genesis generation: %v", stage, err)
		}
		if generationStatus != "active" {
			t.Fatalf("%s: genesis generation status = %q, want active", stage, generationStatus)
		}
		var workItems int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items WHERE scope_id = $1
`, AdvisoryGroupsSyntheticScope).Scan(&workItems); err != nil {
			t.Fatalf("%s: count synthetic work items: %v", stage, err)
		}
		if workItems != 0 {
			t.Fatalf("%s: synthetic scope holds %d work items, want 0 (slots arise from touches)", stage, workItems)
		}
	}

	assertSyntheticAdvisoryAnchor("bootstrap")
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("re-apply bootstrap: %v", err)
	}
	assertSyntheticAdvisoryAnchor("replay")
}
