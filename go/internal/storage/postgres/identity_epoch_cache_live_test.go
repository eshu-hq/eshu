// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestIdentityEpochProbeIsValidSQLLive proves the probe query is valid SQL
// against a real Postgres connection. It seeds a minimal identity fact and
// verifies the probe returns rows without error and the new fact is counted.
// The uncached load is NOT exercised against the full shim (which is large
// and slow); probe validity is the regression we are guarding against.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochProbeIsValidSQLLive(t *testing.T) {
	if os.Getenv("ESHU_IDENTITY_EPOCH_LIVE") != "1" {
		t.Skip("set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	scopeID := "live-epoch-probe-test-" + time.Now().Format("20060102-150405.000")
	genID := "live-epoch-gen-" + time.Now().Format("20060102-150405.000")
	seedIdentityEpochLive(t, sqlDB, scopeID, genID)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	db := SQLDB{DB: sqlDB}
	factStore := NewFactStore(db)

	// Probe must return rows without error.
	epoch, err := factStore.probeIdentityEpoch(context.Background())
	if err != nil {
		t.Fatalf("probeIdentityEpoch error = %v, want nil", err)
	}
	if epoch.count < 1 {
		t.Fatalf("probe count = %d, want >= 1", epoch.count)
	}
	if epoch.activeFingerprint == "" {
		t.Fatalf("probe active_fingerprint = %q, want non-empty (at least one scope seeded)", epoch.activeFingerprint)
	}
	t.Logf("probe passed: count=%d max_obs=%v fingerprint=%s", epoch.count, epoch.maxObservedAt, epoch.activeFingerprint)
}

// TestIdentityEpochProbeDetectsSupersessionLive proves the collision-resistant
// md5 fingerprint (issue #5438 P1-B fix) detects a real active-generation
// supersession against live Postgres. It seeds one ingestion_scope, probes
// the epoch, then flips ONLY that scope's active_generation_id to a new
// generation — a supersession that touches no fact_records rows, so fact
// count and max(observed_at) are provably unchanged by the flip alone — and
// probes again. The fingerprint (and therefore the whole epoch) MUST change,
// or the cache would serve stale identity facts after a supersession with
// this scope shape. This is the regression guard for the bug the old
// sum(hashtext(...)) fingerprint could silently miss (a 32-bit collision or
// offsetting deltas that cancel in the sum).
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochProbeDetectsSupersessionLive(t *testing.T) {
	if os.Getenv("ESHU_IDENTITY_EPOCH_LIVE") != "1" {
		t.Skip("set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()
	scopeID := "live-epoch-supersession-test-" + time.Now().Format("20060102-150405.000")
	genA := "live-epoch-supersession-gen-a-" + time.Now().Format("20060102-150405.000")
	genB := "live-epoch-supersession-gen-b-" + time.Now().Format("20060102-150405.000")

	seedIngestionScopeLive(t, sqlDB, scopeID, genA)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	db := SQLDB{DB: sqlDB}
	factStore := NewFactStore(db)

	epoch1, err := factStore.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch (before supersession): %v", err)
	}

	// Supersede: genA is marked superseded (required by the
	// scope_generations_active_scope_idx UNIQUE partial index, which allows
	// only one 'active' row per scope_id) and genB becomes the new active
	// generation for the SAME scope. No fact_records rows are touched by
	// this step, so count and max(observed_at) are provably unchanged.
	_, err = sqlDB.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE scope_id = $1 AND generation_id = $3`,
		scopeID, now, genA)
	if err != nil {
		t.Fatalf("supersede generation A: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeID, genB, now)
	if err != nil {
		t.Fatalf("seed superseding generation B: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, genB)
	if err != nil {
		t.Fatalf("flip active_generation_id to generation B: %v", err)
	}

	epoch2, err := factStore.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch (after supersession): %v", err)
	}

	if epoch1.count != epoch2.count {
		t.Fatalf("count changed across supersession (test precondition violated): before=%d after=%d", epoch1.count, epoch2.count)
	}
	if !epoch1.maxObservedAt.Equal(epoch2.maxObservedAt) {
		t.Fatalf("max_observed_at changed across supersession (test precondition violated): before=%v after=%v", epoch1.maxObservedAt, epoch2.maxObservedAt)
	}
	if epoch1.activeFingerprint == epoch2.activeFingerprint {
		t.Fatalf("fingerprint did NOT change across active_generation_id supersession: before=%q after=%q — the cache would serve stale identity facts", epoch1.activeFingerprint, epoch2.activeFingerprint)
	}
	if epoch1 == epoch2 {
		t.Fatalf("epoch did NOT change across supersession — the cache would false-hit and serve stale identity facts")
	}
	t.Logf("supersession detected: count=%d max_obs=%v fingerprint before=%q after=%q", epoch1.count, epoch1.maxObservedAt, epoch1.activeFingerprint, epoch2.activeFingerprint)
}

// seedIngestionScopeLive seeds an ingestion_scopes row and its active
// scope_generations row. Split from the fact-seeding step (seedIdentityFactLive)
// so tests that only need a scope/generation shape — for example the
// supersession regression test, which flips active_generation_id without
// touching fact_records — do not have to seed an unused fact.
func seedIngestionScopeLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
		 VALUES ($1, 'repository', 'oci_registry', $1, 'oci_registry', $1, $2, $2, 'active', $3)`,
		scopeID, now, generationID)
	if err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeID, generationID, now)
	if err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}
}

// seedIdentityFactLive seeds one identity fact_records row for scopeID/generationID.
func seedIdentityFactLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	factID := "fact-" + generationID + "-1"
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    schema_version, collector_kind, fencing_token, source_confidence,
		    source_system, source_fact_key, source_uri, source_record_id,
		    observed_at, ingested_at, is_tombstone, payload
		 ) VALUES (
		    $1, $2, $3, 'oci_registry.image_tag_observation', $4,
		    '1.0.0', 'oci_registry', 0, 'reported',
		    'oci_registry', $4, 'oci://example/repo:latest', 'repo:latest',
		    $5, $5, FALSE, '{"registry":"example.com","repository":"repo","tag":"latest"}'::jsonb
		 )`,
		factID, scopeID, generationID, "stable-"+factID, now,
	)
	if err != nil {
		t.Fatalf("seed fact_records: %v", err)
	}
}

// seedIdentityEpochLive seeds a scope, its active generation, and one
// identity fact for that generation.
func seedIdentityEpochLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	seedIngestionScopeLive(t, db, scopeID, generationID)
	seedIdentityFactLive(t, db, scopeID, generationID)
}
