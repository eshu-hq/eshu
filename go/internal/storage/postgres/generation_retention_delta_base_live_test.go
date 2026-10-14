// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionDeltaBaseLive is the #7802 acceptance proof: a
// scope whose active generation is a delta keeps its newest full generation
// (the only record of unchanged files) and the delta chain after it past
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

	// Delta-active scope: active generation is a delta, newest full is a
	// 100-day superseded base, one 95-day superseded delta sits between.
	testfixtures.SeedScope(t, ctx, database, "deltabase-delta")
	markDelta("deltabase-delta-active")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"deltabase-delta", "deltabase-delta-base", now.Add(-100*24*time.Hour))
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

	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (only the active-full scope's old generation)", result.GenerationsPruned)
	}
	remaining := remainingScopeGenerations(t, ctx, database,
		[]string{"deltabase-delta-base", "deltabase-delta-mid", "deltabase-full-old", "deltabase-both-base", "deltabase-both-delta"})
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
		t.Errorf("Skipped[delta_base] = %d, want 3 (two bases plus the covered chain delta)", got)
	}
	if got := result.Skipped["uncovered_writer"]; got != 1 {
		t.Errorf("Skipped[uncovered_writer] = %d, want 1 (overlap row reported exactly once)", got)
	}
}
