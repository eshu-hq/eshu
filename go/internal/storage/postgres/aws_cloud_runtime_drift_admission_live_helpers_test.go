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

// Shared fixtures for the #5848 real-Postgres proofs: the insert-admission
// check (aws_cloud_runtime_drift_admission_live_test.go) and the readiness
// defer (aws_cloud_runtime_drift_readiness_live_test.go).
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run 'AWSCloudRuntimeDrift.*Live' -count=1 -v

// awsCloudRuntimeDriftAdmissionLiveDB opens the DSN-gated database and applies
// the bootstrap schema, skipping the whole test when no DSN is configured.
// Mirrors containerImageIdentityFenceLiveDB's split-budget rationale: schema
// setup gets its own deadline so cold DDL under host load cannot eat the
// proof's own budget.
func awsCloudRuntimeDriftAdmissionLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres aws_cloud_runtime_drift #5848 proofs")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	err = ApplyBootstrap(schemaCtx, SQLDB{DB: sqlDB})
	cancelSchema()
	if err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return sqlDB, ctx
}

// seedAWSCloudRuntimeDriftScope inserts (or updates) one ingestion_scopes row.
// activeGenerationID may be empty to leave the scope with no active generation
// (the pre-activation shape the readiness defer targets).
func seedAWSCloudRuntimeDriftScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	scopeKind string,
	activeGenerationID string,
	now time.Time,
) {
	t.Helper()

	var activeGen any
	if activeGenerationID != "" {
		activeGen = activeGenerationID
	}
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, $2, 'aws', $1, 'aws', $1, $3, $3, 'active', $4, '{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
		scopeID, scopeKind, now, activeGen,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
}

// seedAWSCloudRuntimeDriftGeneration inserts one scope_generations row at the
// given status ('pending', 'active', or 'failed').
func seedAWSCloudRuntimeDriftGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	generationID string,
	scopeID string,
	status string,
	now time.Time,
) {
	t.Helper()

	var activatedAt any
	if status == "active" {
		activatedAt = now
	}
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, $4, $5)
		ON CONFLICT (generation_id) DO UPDATE SET status = EXCLUDED.status, activated_at = EXCLUDED.activated_at`,
		generationID, scopeID, now, status, activatedAt,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", generationID, err)
	}
}

// countAWSCloudRuntimeDriftFindingRows returns the finding_kind of every
// active reducer_aws_cloud_runtime_drift_finding row for scope/generation,
// ordered by fact_id for determinism.
func countAWSCloudRuntimeDriftFindingRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) []string {
	t.Helper()

	rows, err := db.QueryContext(
		ctx, `
		SELECT payload->>'finding_kind'
		FROM fact_records
		WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3
		ORDER BY fact_id ASC`,
		scopeID, generationID, AWSCloudRuntimeDriftFindingFactKind,
	)
	if err != nil {
		t.Fatalf("query aws cloud runtime drift findings: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var kinds []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan finding_kind: %v", err)
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate aws cloud runtime drift findings: %v", err)
	}
	return kinds
}
