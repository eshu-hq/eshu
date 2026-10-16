// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionDeltaBaseLive is the #7802 acceptance proof: a
// scope whose active generation is a delta keeps its newest activated full
// generation (the only record of unchanged files) and the delta chain after it past
// the hard history ceiling, while a scope whose active generation is a full
// snapshot still prunes old superseded generations. The retention summary
// counts the kept delta-base rows so an operator can see them. Fixture ages
// (100d, 95d) sit far past the 90-day ceiling so clock skew cannot flip the
// boundary.
func TestGenerationRetentionDeltaBaseLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()

	markDelta := func(generationID string) {
		t.Helper()
		if _, err := database.ExecContext(ctx,
			`UPDATE scope_generations SET is_delta = true WHERE generation_id = $1`, generationID); err != nil {
			t.Fatalf("mark %s delta: %v", generationID, err)
		}
	}
	// A real base full was always activated before it was superseded
	// (activation sets activated_at and superseding keeps it), and the
	// delta_base definition reads only activated fulls. The shared
	// superseded seeder leaves activated_at NULL, so mark each base.
	activate := func(generationID string) {
		t.Helper()
		markRetentionGenerationActivated(t, ctx, database, generationID)
	}

	// Delta-active scope: active generation is a delta, the newest activated
	// full is a 100-day superseded base, and one 95-day superseded delta
	// sits between.
	testfixtures.SeedScope(t, ctx, database, "deltabase-delta")
	markDelta("deltabase-delta-active")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-delta", "deltabase-delta-base", now.Add(-100*24*time.Hour))
	activate("deltabase-delta-base")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-delta", "deltabase-delta-mid", now.Add(-95*24*time.Hour))
	markDelta("deltabase-delta-mid")

	// Overlap scope: the chain delta also meets the uncovered-writer
	// definition (wrote, never activated, no activated full in scope), so
	// it is both uncovered and delta-protected and must report exactly
	// once, on the uncovered leg.
	testfixtures.SeedScope(t, ctx, database, "deltabase-both")
	markDelta("deltabase-both-active")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-both", "deltabase-both-base", now.Add(-100*24*time.Hour))
	activate("deltabase-both-base")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-both", "deltabase-both-delta", now.Add(-95*24*time.Hour))
	markDelta("deltabase-both-delta")
	if _, err := database.ExecContext(ctx,
		`UPDATE scope_generations SET projection_write_started_at = $2 WHERE generation_id = $1`,
		"deltabase-both-delta", now.Add(-94*24*time.Hour)); err != nil {
		t.Fatalf("mark deltabase-both-delta writing: %v", err)
	}

	// Active-full scope: a 100-day superseded full must still prune.
	testfixtures.SeedScope(t, ctx, database, "deltabase-full")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-full", "deltabase-full-old", now.Add(-100*24*time.Hour))

	// Same-SHA scope (#7981 with #7802): the active generation is a delta,
	// the newest activated full is a protected base, and an older full of
	// the same commit sits inside the count window. The same-SHA rule prunes
	// the older copy because the protected base is its newer witness (the
	// witness read is independent of the delta leg); the base itself stays.
	// It is inside the window and has no newer copy, so it is not eligible
	// and reports nothing.
	testfixtures.SeedScope(t, ctx, database, "deltabase-dup")
	markDelta("deltabase-dup-active")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-dup", "deltabase-dup-old", now.Add(-20*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-dup", "deltabase-dup-base", now.Add(-10*24*time.Hour))
	activate("deltabase-dup-old")
	activate("deltabase-dup-base")
	if _, err := database.ExecContext(ctx,
		`UPDATE scope_generations SET source_commit_sha = 'deltabase-dup-sha'
		  WHERE generation_id IN ('deltabase-dup-old', 'deltabase-dup-base')`); err != nil {
		t.Fatalf("mark same-SHA copies: %v", err)
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
		PolicyRevision:           "7802-delta-base-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}

	if result.GenerationsPruned != 2 {
		t.Errorf("GenerationsPruned = %d, want 2 (the active-full scope's old generation and the same-SHA older copy)", result.GenerationsPruned)
	}
	remaining := remainingScopeGenerations(t, ctx, database,
		[]string{
			"deltabase-delta-base", "deltabase-delta-mid", "deltabase-full-old", "deltabase-both-base",
			"deltabase-both-delta", "deltabase-dup-old", "deltabase-dup-base",
		})
	if remaining["deltabase-dup-old"] {
		t.Error("same-SHA older copy was retained, want pruned against the protected base witness")
	}
	if !remaining["deltabase-dup-base"] {
		t.Error("same-SHA protected base was pruned, want kept")
	}
	if !remaining["deltabase-delta-base"] {
		t.Error("delta-active base full was pruned past the hard ceiling, want kept whatever the age")
	}
	if !remaining["deltabase-delta-mid"] {
		t.Error("delta-active chain delta was pruned past the hard ceiling, want kept with its base")
	}
	if remaining["deltabase-full-old"] {
		t.Error("active-full old superseded generation was retained, want pruned (no behavior change)")
	}
	if !remaining["deltabase-both-base"] {
		t.Error("overlap scope base full was pruned, want kept")
	}
	if !remaining["deltabase-both-delta"] {
		t.Error("overlap scope chain delta was pruned, want kept")
	}
	if got := result.Skipped["delta_base"]; got != 3 {
		t.Errorf("Skipped[delta_base] = %d, want 3 (two bases plus the covered chain delta; the in-window same-SHA base is not eligible, so it is not reported)", got)
	}
	if got := result.Skipped[sameSHANewestSkippedReason]; got != 0 {
		t.Errorf("Skipped[%s] = %d, want 0 (a protected base is never a witness-retained copy)", sameSHANewestSkippedReason, got)
	}
	if got := result.Skipped["uncovered_writer"]; got != 1 {
		t.Errorf("Skipped[uncovered_writer] = %d, want 1 (overlap row reported exactly once)", got)
	}
}

// TestGenerationRetentionDeltaBaseSkipsUnactivatedNewerFullLive pins the base
// to the newest ACTIVATED full (#7802 review F1). A newer full that never
// activated (status failed after a dead letter, or pending until its
// projection acks) carries no rebuildable truth, so it must not displace the
// real base of the active delta. If it did, the real base would lose its
// protection and prune past the hard ceiling.
func TestGenerationRetentionDeltaBaseSkipsUnactivatedNewerFullLive(t *testing.T) {
	for _, status := range []string{"failed", "pending"} {
		t.Run(status, func(t *testing.T) {
			bridgeRetentionFamilyDSN(t)
			database, ctx := openGenerationRetentionMigratedSchema(t)
			now := time.Now().UTC()
			const scopeID = "deltabase-unactivated"
			testfixtures.SeedScope(t, ctx, database, scopeID)
			// Real base: a full superseded 100 days ago that was activated.
			testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-base", now.Add(-100*24*time.Hour))
			markRetentionGenerationActivated(t, ctx, database, scopeID+"-base")
			if _, err := database.ExecContext(ctx,
				`UPDATE scope_generations SET is_delta = true, activated_at = $2 WHERE generation_id = $1`,
				scopeID+"-active", now); err != nil {
				t.Fatalf("mark active delta: %v", err)
			}
			// A newer full that never activated.
			if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, is_delta)
VALUES ($1, $2, 'snapshot', $3, $3, $4, false)`,
				scopeID+"-newer-full", scopeID, now.Add(time.Minute), status); err != nil {
				t.Fatalf("seed newer %s full: %v", status, err)
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
				PolicyRevision:           "7802-unactivated-newer-full-live",
			})
			if err != nil {
				t.Fatalf("PruneSupersededGenerations() error = %v", err)
			}
			remaining := remainingScopeGenerations(t, ctx, database, []string{scopeID + "-base"})
			if !remaining[scopeID+"-base"] {
				t.Errorf("activated base of the active delta was pruned while a newer %s full exists, want kept", status)
			}
			if result.GenerationsPruned != 0 {
				t.Errorf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
			}
			if got := result.Skipped["delta_base"]; got != 1 {
				t.Errorf("Skipped[delta_base] = %d, want 1 (the activated base)", got)
			}
		})
	}
}

// markRetentionGenerationActivated sets activated_at to ingested_at, the
// state activation leaves on a full before a newer generation supersedes it.
func markRetentionGenerationActivated(t *testing.T, ctx context.Context, database *sql.DB, generationID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx,
		`UPDATE scope_generations SET activated_at = ingested_at WHERE generation_id = $1`, generationID); err != nil {
		t.Fatalf("activate %s: %v", generationID, err)
	}
}
