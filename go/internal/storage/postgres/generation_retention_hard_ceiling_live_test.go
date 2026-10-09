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

// TestGenerationRetentionHardCeilingLive is the #7585 boundary proof: the
// hard history ceiling caps the count preference. A superseded generation
// older than the ceiling is pruned even though its rank (1) is inside the
// retained count of 24, while a rank-1 generation inside the ceiling is
// retained. A second pass with an unset ceiling proves the default keeps 90
// days. Fixture ages (100d vs 10d) sit far from both the 7-day soft edge and
// the 90-day ceiling so clock skew cannot flip the boundary.
func TestGenerationRetentionHardCeilingLive(t *testing.T) {
	// Bridge the live-postgres-readiness runner's family DSN onto the shared
	// helper's generic variable. Local runs keep using ESHU_POSTGRES_TEST_DSN
	// directly; the runner sets only the family pair, and a family DSN
	// without its disposable acknowledgment fails closed.
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()

	policyWith := func(hardMax time.Duration) GenerationRetentionPolicy {
		return GenerationRetentionPolicy{
			MinSupersededGenerations: 24,
			MaxSupersededAge:         7 * 24 * time.Hour,
			HardMaxSupersededAge:     hardMax,
			BatchGenerationLimit:     10,
			BatchRowLimit:            1_000_000_000,
			PolicyScope:              "global",
			PolicyRevision:           "7585-hard-ceiling-live",
		}
	}
	prune := func(policy GenerationRetentionPolicy) GenerationRetentionResult {
		t.Helper()
		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(ctx, policy)
		if err != nil {
			t.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		return result
	}

	testfixtures.SeedScope(t, ctx, database, "hardceiling-beyond")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"hardceiling-beyond", "hardceiling-beyond-g0", now.Add(-100*24*time.Hour))
	testfixtures.SeedScope(t, ctx, database, "hardceiling-inside")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"hardceiling-inside", "hardceiling-inside-g0", now.Add(-10*24*time.Hour))

	// Configured ceiling honored: the 100-day rank-1 generation is pruned
	// despite the count of 24, and the 10-day rank-1 generation is retained.
	if result := prune(policyWith(90 * 24 * time.Hour)); result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (only the beyond-ceiling generation)", result.GenerationsPruned)
	}
	remaining := remainingScopeGenerations(t, ctx, database,
		[]string{"hardceiling-beyond-g0", "hardceiling-inside-g0"})
	if remaining["hardceiling-beyond-g0"] {
		t.Error("beyond-ceiling generation was retained, want pruned despite rank 1 of 24")
	}
	if !remaining["hardceiling-inside-g0"] {
		t.Error("inside-ceiling rank-1 generation was pruned, want retained by the count window")
	}

	// Unset ceiling keeps 90 days: a fresh 100-day rank-1 generation is
	// pruned under the zero-value policy the same way.
	testfixtures.SeedScope(t, ctx, database, "hardceiling-default")
	testfixtures.SeedSupersededGeneration(t, ctx, database,
		"hardceiling-default", "hardceiling-default-g0", now.Add(-100*24*time.Hour))
	if result := prune(policyWith(0)); result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (unset ceiling keeps the 90-day default)", result.GenerationsPruned)
	}
	if remaining := remainingScopeGenerations(t, ctx, database,
		[]string{"hardceiling-default-g0"}); remaining["hardceiling-default-g0"] {
		t.Error("beyond-ceiling generation was retained under the unset ceiling, want pruned by the 90-day default")
	}
}
