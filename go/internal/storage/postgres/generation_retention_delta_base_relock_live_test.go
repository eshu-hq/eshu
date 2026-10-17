// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7802 disposable PostgreSQL 18 proof that the targeted re-lock keeps a chain delta that became delta-protected between selection and re-lock, and still prunes a bystander older than the base, with and without a newer pending full, enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the hard-ceiling proof does. local run passed in 1.20s wall with the full 196-migration setup on native PostgreSQL 18.6 on 2026-10-15
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionRelockKeepsFlippedDeltaChainLive is the behavioral
// proof of the targeted re-lock's delta guard (#7802 review F2). At
// selection the scope's active generation is a full, so an old superseded
// delta after it is ordinary prunable history. Between selection and
// re-lock a new delta activates. The old active full becomes the delta
// base, and the old delta after it becomes a protected chain row. The
// re-lock must exclude that row from the lock set and report it as
// Skipped["delta_base"], while a bystander full older than the base still
// prunes. The newer_pending_full case adds a newer full that never
// activated. The re-lock must still take the old activated full as the
// base (#7802 review F1), so the chain delta stays protected.
func TestGenerationRetentionRelockKeepsFlippedDeltaChainLive(t *testing.T) {
	for _, tc := range []struct {
		name             string
		newerPendingFull bool
	}{
		{name: "no_newer_full"},
		{name: "newer_pending_full", newerPendingFull: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relockKeepsFlippedDeltaChain(t, tc.newerPendingFull)
		})
	}
}

// relockKeepsFlippedDeltaChain runs one case of
// TestGenerationRetentionRelockKeepsFlippedDeltaChainLive.
func relockKeepsFlippedDeltaChain(t *testing.T, newerPendingFull bool) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	const scopeID = "deltabase-relock"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	// The active full at selection, activated 200 days ago.
	exec(`UPDATE scope_generations SET ingested_at = $2, observed_at = $2, activated_at = $2 WHERE generation_id = $1`,
		scopeID+"-active", now.Add(-200*24*time.Hour))
	// Chain delta: ingested after the active full, superseded past the
	// 90-day ceiling. Prunable at selection, protected after the flip.
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-chain", now.Add(-100*24*time.Hour))
	exec(`UPDATE scope_generations SET is_delta = true WHERE generation_id = $1`, scopeID+"-chain")
	// Bystander: an activated full older than the base. Prunable before
	// and after the flip.
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-old", now.Add(-300*24*time.Hour))
	markRetentionGenerationActivated(t, ctx, database, scopeID+"-old")

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	store.beforeTargetedLock = func() {
		// A delta activates between selection and re-lock: the old full
		// is superseded and becomes the base of the new active delta.
		exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = $1`,
			scopeID+"-active")
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, is_delta, activated_at)
VALUES ($1, $2, 'snapshot', now(), now(), 'active', true, now())`, scopeID+"-delta", scopeID)
		exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, scopeID+"-delta", scopeID)
		if newerPendingFull {
			exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, is_delta)
VALUES ($1, $2, 'snapshot', now(), now(), 'pending', false)`, scopeID+"-pending-full", scopeID)
		}
	}
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7802-relock-delta-flip-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}

	remaining := remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-chain", scopeID + "-old", scopeID + "-active",
	})
	if !remaining[scopeID+"-chain"] {
		t.Error("chain delta that became protected before the re-lock was pruned, want kept")
	}
	if remaining[scopeID+"-old"] {
		t.Error("bystander full older than the base was kept, want pruned")
	}
	if !remaining[scopeID+"-active"] {
		t.Error("new delta base was pruned, want kept (superseded just now, not selected)")
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (the bystander only)", result.GenerationsPruned)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Errorf("Skipped[delta_base] = %d, want 1 (the flipped chain delta, counted once)", got)
	}
}
