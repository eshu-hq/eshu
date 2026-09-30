// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
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

// Twin copies of this helper and seedAWSCloudRuntimeDriftGeneration below live in
// scope/completion/quiescence_live_test.go (Go test-only symbols do not cross
// package boundaries); keep each pair behavior-identical.
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

// awsCloudRuntimeDriftIsolatedLiveDB is awsCloudRuntimeDriftAdmissionLiveDB in
// a schema of its own, dropped when the test ends. A proof that drives
// ReducerQueue.Claim must use it: Claim takes the oldest claimable reducer row
// in the database, so on the shared schema it can take a row that a sibling
// test or an earlier run left behind, and the proof then acks and reopens the
// wrong work item (#7479). Tables land in the isolated schema, which comes
// first on the search_path; public stays on it so the pg_trgm extension the
// bootstrap needs resolves there.
func awsCloudRuntimeDriftIsolatedLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres aws_cloud_runtime_drift #5848 proofs")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelSchema()
	installGenerationRetentionTrigramExtension(schemaCtx, t, admin)
	schema := fmt.Sprintf("aws_drift_live_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(schemaCtx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	sqlDB, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := ApplyBootstrap(schemaCtx, SQLDB{DB: sqlDB}); err != nil {
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
