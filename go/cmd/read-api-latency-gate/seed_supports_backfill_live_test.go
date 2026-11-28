// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11658 seeded supports-backfill stand-in on real PostgreSQL: the gate's seeded corpus (maintainer-written supports, zero finding facts in fact_records under the reject trigger) must read not-ready before the stand-in and ready after backfillSeededSupportsReadModel, through the same sql.Open/pgx DSN path the seed uses
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// seededSupportsLiveScopes anchors the seeded corpus on two active
// generations, so BuildWinnersFindingFacts spreads finding groups across
// both and the corpus stays multi-pair like the gate seed's plan anchors.
var seededSupportsLiveScopes = []winnersFactAnchor{
	{ScopeID: "scope-seeded-supports-live-a", GenerationID: "gen-seeded-supports-live-a-1"},
	{ScopeID: "scope-seeded-supports-live-b", GenerationID: "gen-seeded-supports-live-b-1"},
}

func openSeededSupportsLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set; skipping the seeded supports-backfill proof")
	}
	admin, err := postgresproof.WithDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("derive administrative DSN: %v", err)
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, admin, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	for _, anchor := range seededSupportsLiveScopes {
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, anchor.ScopeID, now); err != nil {
			t.Fatalf("insert scope %s: %v", anchor.ScopeID, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, anchor.GenerationID, anchor.ScopeID, now); err != nil {
			t.Fatalf("insert generation %s: %v", anchor.GenerationID, err)
		}
		if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			anchor.GenerationID, anchor.ScopeID); err != nil {
			t.Fatalf("activate generation %s: %v", anchor.GenerationID, err)
		}
	}
	return ctx, database
}

func countSeededSupportsLive(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM supply_chain_impact_finding_supports`).Scan(&count); err != nil {
		t.Fatalf("count seeded supports: %v", err)
	}
	return count
}

// TestBackfillSeededSupportsReadModelDrivesSeededSupportsToReadyLive proves
// the wired stand-in on PostgreSQL: a seeded-shaped corpus (maintainer-written
// supports, no finding facts) reads not-ready like the 503 state the gate hit,
// and backfillSeededSupportsReadModel — through its own sql.Open/pgx wiring,
// the product RunStep at default chunk size, and the 50-step budget — drives
// the serving readiness predicate to true.
func TestBackfillSeededSupportsReadModelDrivesSeededSupportsToReadyLive(t *testing.T) {
	ctx, database := openSeededSupportsLiveDB(t)
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	facts := BuildWinnersFindingFacts(seededSupportsLiveScopes, 2*winnersSourcesPerGroup, winnersSourcesPerGroup)
	if len(facts) == 0 {
		t.Fatal("built no winners finding facts; the seeded corpus would be vacuously empty")
	}
	if err := SeedWinnersSupports(ctx, database, facts, now); err != nil {
		t.Fatalf("seed winners supports: %v", err)
	}
	store := postgres.SQLDB{DB: database}
	if got := countSeededSupportsLive(t, ctx, database); got == 0 {
		t.Fatal("seeded supports = 0: the readiness assertions below would pass vacuously on an empty corpus")
	}
	// RED half: without the stand-in this corpus is exactly the 503 state the
	// gate measured — supports present, marker unset, readiness false.
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || ready {
		t.Fatalf("ready before the backfill = %v (err %v), want false", ready, err)
	}

	var proofDBName string
	if err := database.QueryRowContext(ctx, `SELECT current_database()`).Scan(&proofDBName); err != nil {
		t.Fatalf("learn disposable database name: %v", err)
	}
	targetDSN, err := postgresproof.WithDatabase(os.Getenv("ESHU_POSTGRES_DSN"), proofDBName)
	if err != nil {
		t.Fatalf("derive disposable DSN: %v", err)
	}
	var log bytes.Buffer
	if err := backfillSeededSupportsReadModel(ctx, targetDSN, &log); err != nil {
		t.Fatalf("backfillSeededSupportsReadModel: %v", err)
	}
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready after the backfill = %v (err %v), want true", ready, err)
	}
	if !strings.Contains(log.String(), "supports read model backfilled") {
		t.Fatalf("backfill log %q misses the completion line", log.String())
	}
}
