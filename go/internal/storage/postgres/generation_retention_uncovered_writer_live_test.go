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

// TestGenerationRetentionKeepsUncoveredWritersLive is #7472: a superseded
// generation that wrote the graph and never activated, newer than the scope's
// last activated full, is exactly what the collector's reconciliation probe
// (UncoveredProjectionWriters) needs to force a full snapshot — retention
// must not prune it, at any age. A covered writer (older than the last
// activated full) carries no such evidence and must still prune, so the
// exclusion mirrors the probe instead of over-retaining. A second scope with
// no activated full pins the COALESCE '-infinity' default through this query:
// with no horizon, every writer is uncovered and must be retained and counted
// as skipped.
func TestGenerationRetentionKeepsUncoveredWritersLive(t *testing.T) {
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
	const scopeID = "retention-uncovered-01"
	testfixtures.SeedScope(t, ctx, database, scopeID)

	old := now.Add(-2 * time.Hour)
	recent := now.Add(-30 * time.Minute)
	coveredWrite := now.Add(-120 * time.Minute)
	fullWrite := now.Add(-100 * time.Minute)
	uncoveredWrite := now.Add(-80 * time.Minute)

	// The coverage horizon: an activated full, too recent to be eligible
	// itself, so it survives the pass and the horizon is stable.
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-full", generationWriterSeed{
		supersededAt: recent, ingestedAt: recent, activatedAt: recent,
		writeStartedAt: fullWrite, isDelta: false,
	})
	// Uncovered: superseded, old, wrote after the last activated full.
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-uncovered", generationWriterSeed{
		supersededAt: old, ingestedAt: old,
		writeStartedAt: uncoveredWrite, isDelta: false,
	})
	// Covered control: superseded, old, wrote before the last activated
	// full (a delta, so both writer shapes are pinned).
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-covered", generationWriterSeed{
		supersededAt: old, ingestedAt: old,
		writeStartedAt: coveredWrite, isDelta: true,
	})

	// Fail-closed scope: no activated full at all, so the horizon
	// subquery has no row and the '-infinity' default fires. The old
	// writer is uncovered by definition and must be retained and counted.
	const bareScopeID = "retention-uncovered-bare"
	testfixtures.SeedScope(t, ctx, database, bareScopeID)
	seedUncoveredWriterGeneration(t, ctx, database, bareScopeID, bareScopeID+"-writer", generationWriterSeed{
		supersededAt: old, ingestedAt: old,
		writeStartedAt: old, isDelta: false,
	})

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7472-uncovered-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}

	remaining := remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-full", scopeID + "-uncovered", scopeID + "-covered",
		bareScopeID + "-writer",
	})
	if !remaining[scopeID+"-full"] {
		t.Errorf("coverage-horizon full %s was pruned, want retained (too recent to be eligible)", scopeID+"-full")
	}
	if !remaining[scopeID+"-uncovered"] {
		t.Errorf("uncovered writer %s was pruned, want retained (the reconcile probe needs it)", scopeID+"-uncovered")
	}
	if remaining[scopeID+"-covered"] {
		t.Errorf("covered writer %s was retained, want pruned (older than the last activated full)", scopeID+"-covered")
	}
	if !remaining[bareScopeID+"-writer"] {
		t.Errorf("fail-closed writer %s was pruned, want retained (no activated full, so the '-infinity' default must fire)", bareScopeID+"-writer")
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (the covered writer only)", result.GenerationsPruned)
	}
	if got := result.Skipped["uncovered_writer"]; got != 2 {
		t.Errorf(`Skipped["uncovered_writer"] = %d, want 2 (the uncovered writer plus the fail-closed writer)`, got)
	}
}

// generationWriterSeed describes one scope_generations row with an explicit
// projection-write marker. A zero activatedAt leaves the generation
// unactivated; a zero writeStartedAt leaves the marker NULL.
type generationWriterSeed struct {
	supersededAt   time.Time
	ingestedAt     time.Time
	activatedAt    time.Time
	writeStartedAt time.Time
	isDelta        bool
}

// seedUncoveredWriterGeneration inserts one superseded generation with an
// explicit write marker, activation, and delta shape.
func seedUncoveredWriterGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, seed generationWriterSeed) {
	t.Helper()
	var activatedAt, writeStartedAt any
	if !seed.activatedAt.IsZero() {
		activatedAt = seed.activatedAt
	}
	if !seed.writeStartedAt.IsZero() {
		writeStartedAt = seed.writeStartedAt
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    superseded_at, activated_at, is_delta, projection_write_started_at)
VALUES ($1, $2, 'snapshot', $3, $4, 'superseded', $3, $5, $6, $7)`,
		generationID, scopeID, seed.supersededAt, seed.ingestedAt, activatedAt, seed.isDelta, writeStartedAt,
	); err != nil {
		t.Fatalf("seed writer generation %s: %v", generationID, err)
	}
}
