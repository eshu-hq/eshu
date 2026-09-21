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

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPriorGenerationIDStrictlyOlderLive is the #6892 P1 regression proof
// over a real Postgres: with gen6 < gen7 < gen8 by observed_at, resolving
// the prior of the non-newest gen7 must return gen6 — the old newest-other
// predicate returned gen8, so a re-run gen7 intent diffed against a NEWER
// generation and yielded newly-created uids as retract candidates.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_PROVE_LIVE=1 and
// ESHU_CLOUD_RETRACT_PG_DSN (the same live gate as the #6887 retract proof).
func TestPriorGenerationIDStrictlyOlderLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_PROVE_LIVE=1 (+ ESHU_CLOUD_RETRACT_PG_DSN) to run the prior-generation live proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_CLOUD_RETRACT_PG_DSN is required")
	}
	ctx := context.Background()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()

	prefix := "prior-live"
	now := time.Now().UTC().Truncate(time.Millisecond)
	gen6, gen7, gen8 := prefix+"-gen6", prefix+"-gen7", prefix+"-gen8"
	seen := map[string]time.Time{
		gen6: now.Add(-2 * time.Hour),
		gen7: now.Add(-1 * time.Hour),
		gen8: now,
	}
	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, err := rawDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	execSQL(`
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', '{}'::jsonb)
ON CONFLICT (scope_id) DO NOTHING`,
		prefix+"-scope", now)
	for gen, at := range seen {
		execSQL(`
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'manual', $3, $3, 'pending')
ON CONFLICT (generation_id) DO UPDATE SET observed_at = EXCLUDED.observed_at`,
			gen, prefix+"-scope", at)
	}
	defer func() {
		execSQL(`DELETE FROM scope_generations WHERE scope_id = $1`, prefix+"-scope")
		execSQL(`DELETE FROM ingestion_scopes WHERE scope_id = $1`, prefix+"-scope")
	}()

	priorOf := NewPriorGenerationID(SQLDB{DB: rawDB})

	got, found, err := priorOf(ctx, prefix+"-scope", gen7)
	if err != nil {
		t.Fatalf("prior(gen7): %v", err)
	}
	if !found || got != gen6 {
		t.Fatalf("prior(gen7) = %q, found=%v, want %q (newer gen8 must never resolve as prior)", got, found, gen6)
	}

	got, found, err = priorOf(ctx, prefix+"-scope", gen8)
	if err != nil {
		t.Fatalf("prior(gen8): %v", err)
	}
	if !found || got != gen7 {
		t.Fatalf("prior(gen8) = %q, found=%v, want %q", got, found, gen7)
	}

	if _, found, err = priorOf(ctx, prefix+"-scope", gen6); err != nil {
		t.Fatalf("prior(gen6): %v", err)
	} else if found {
		t.Fatal("prior(gen6) found=true, want false (oldest generation has no prior)")
	}

	if _, found, err = priorOf(ctx, prefix+"-scope", prefix+"-missing"); err != nil {
		t.Fatalf("prior(missing): %v", err)
	} else if found {
		t.Fatal("prior(missing) found=true, want false (unknown intent row skips the retract)")
	}
}
