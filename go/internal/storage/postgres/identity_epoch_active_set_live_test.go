// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// TestIdentityEpochIgnoresSupersededGenerationRowsLive is the #7805 epoch
// regression against real Postgres. Deleting an identity fact of a superseded
// generation (what retention does every few minutes) must leave the epoch
// unchanged, while inserting or deleting an identity fact of the active
// generation must change it.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochIgnoresSupersededGenerationRowsLive(t *testing.T) {
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
	stamp := time.Now().Format("20060102-150405.000")
	scopeID := "live-epoch-active-set-" + stamp
	activeGen := "live-epoch-active-gen-" + stamp
	oldGen := "live-epoch-old-gen-" + stamp

	seedIngestionScopeLive(t, sqlDB, scopeID, activeGen)
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
		 VALUES ($1, $2, 'manual', $3, $3, 'superseded', $3)`,
		scopeID, oldGen, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("seed superseded generation: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM fact_records WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	seedIdentityFactLive(t, sqlDB, scopeID, oldGen)
	seedIdentityFactLive(t, sqlDB, scopeID, activeGen)
	store := NewFactStore(SQLDB{DB: sqlDB})

	baseline, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("baseline probe: %v", err)
	}

	// Retention-style delete on the superseded generation: epoch must not move.
	if _, err := sqlDB.ExecContext(ctx,
		`DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`, scopeID, oldGen); err != nil {
		t.Fatalf("delete superseded-generation fact: %v", err)
	}
	afterSupersededDelete, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after superseded delete: %v", err)
	}
	if afterSupersededDelete != baseline {
		t.Fatalf("deleting a superseded-generation fact moved the epoch: before=%+v after=%+v", baseline, afterSupersededDelete)
	}

	// An insert on the active generation must move the epoch.
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
		    collector_kind, fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ($1, $2, $3, 'oci_registry.image_tag_observation', $1, '1.0.0', 'oci_registry', 0, 'reported',
		    'oci_registry', $1, $4, $4, FALSE, '{}'::jsonb)`,
		"fact-extra-"+stamp, scopeID, activeGen, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("insert active-generation fact: %v", err)
	}
	afterInsert, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after active insert: %v", err)
	}
	if afterInsert == afterSupersededDelete {
		t.Fatalf("inserting an active-generation fact did not move the epoch: %+v", afterInsert)
	}

	// A delete on the active generation must move the epoch back off the insert.
	if _, err := sqlDB.ExecContext(ctx,
		`DELETE FROM fact_records WHERE fact_id = $1`, "fact-extra-"+stamp); err != nil {
		t.Fatalf("delete active-generation fact: %v", err)
	}
	afterActiveDelete, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after active delete: %v", err)
	}
	if afterActiveDelete == afterInsert {
		t.Fatalf("deleting an active-generation fact did not move the epoch: %+v", afterActiveDelete)
	}
}
