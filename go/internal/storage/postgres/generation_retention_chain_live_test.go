// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8780 disposable PostgreSQL 18 proof that retention keeps a cumulative delta chain's union base: with F, a superseded delta d1, and an active cumulative delta d2 whose union base is F, F and d1 survive past the hard ceiling as delta_base; bridges ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the #7802 proof does
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionKeepsCumulativeDeltaBaseLive extends #7802 to the
// #8780 chain: the collector picks the union base by the same rule retention
// protects (the newest activated full), so recording it on the deltas needs no
// retention change. F (100 days superseded), d1 (95 days, a superseded delta
// fenced on F), and the active d2 (fenced on d1, union base F) keep F and d1.
func TestGenerationRetentionKeepsCumulativeDeltaBaseLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID = "deltabase-chain"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-f", now.Add(-100*24*time.Hour))
	markRetentionGenerationActivated(t, ctx, database, scopeID+"-f")
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-d1", now.Add(-95*24*time.Hour))
	markRetentionGenerationActivated(t, ctx, database, scopeID+"-d1")
	for _, step := range []struct{ query string }{
		{`UPDATE scope_generations SET source_commit_sha = 'sha-f' WHERE generation_id = 'deltabase-chain-f'`},
		{`UPDATE scope_generations SET is_delta = true, source_commit_sha = 'sha-d1',
		    delta_baseline_commit_sha = 'sha-f', delta_union_base_commit_sha = 'sha-f'
		  WHERE generation_id = 'deltabase-chain-d1'`},
		{`UPDATE scope_generations SET is_delta = true, activated_at = now(), source_commit_sha = 'sha-d2',
		    delta_baseline_commit_sha = 'sha-d1', delta_union_base_commit_sha = 'sha-f'
		  WHERE generation_id = 'deltabase-chain-active'`},
	} {
		if _, err := database.ExecContext(ctx, step.query); err != nil {
			t.Fatalf("shape the chain: %v", err)
		}
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "8780-cumulative-delta-base-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	remaining := remainingScopeGenerations(t, ctx, database, []string{scopeID + "-f", scopeID + "-d1"})
	if !remaining[scopeID+"-f"] || !remaining[scopeID+"-d1"] {
		t.Fatalf("remaining = %v, want the union base F and the chain delta d1 kept past the hard ceiling", remaining)
	}
	if result.GenerationsPruned != 0 {
		t.Errorf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if got := result.Skipped["delta_base"]; got != 2 {
		t.Errorf("Skipped[delta_base] = %d, want 2 (the union base and the chain delta)", got)
	}
}
