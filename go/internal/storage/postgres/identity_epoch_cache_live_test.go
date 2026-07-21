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
	if epoch.activeFingerprint == 0 {
		t.Fatalf("probe active_fingerprint = 0, want non-zero (at least one scope seeded)")
	}
	t.Logf("probe passed: count=%d max_obs=%v fingerprint=%d", epoch.count, epoch.maxObservedAt, epoch.activeFingerprint)
}

func seedIdentityEpochLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
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

	factID := "fact-" + generationID + "-1"
	_, err = db.ExecContext(
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
