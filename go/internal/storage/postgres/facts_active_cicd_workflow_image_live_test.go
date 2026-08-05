// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestListActiveCICDWorkflowImageFactsOwnerLifecycleLive(t *testing.T) {
	const schema = "eshu_5703_workflow_image_bridge_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5703 workflow-image bridge proof")
	}
	db := openCICDWorkflowImageLiveDB(t, dsn, schema)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, cicdWorkflowImageLiveSchemaAndFixture); err != nil {
		t.Fatalf("seed workflow-image bridge fixture: %v", err)
	}
	loaded, err := NewFactStore(SQLDB{DB: db}).ListActiveCICDWorkflowImageFacts(
		ctx,
		[]string{"repository:owner"},
	)
	if err != nil {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() error = %v, want nil", err)
	}
	factIDs := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		factIDs = append(factIDs, envelope.FactID)
	}
	if want := []string{"workflow-default", "workflow-ref"}; !slices.Equal(factIDs, want) {
		t.Fatalf("fact IDs = %#v, want active owned default+ref facts %#v", factIDs, want)
	}
}

func openCICDWorkflowImageLiveDB(t *testing.T, dsn, schema string) *sql.DB {
	t.Helper()
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const cicdWorkflowImageLiveSchemaAndFixture = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL
);
CREATE INDEX ingestion_scopes_source_idx
    ON ingestion_scopes (source_system, scope_kind, partition_key, scope_id);
CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    status TEXT NOT NULL
);
CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '1.0.0',
    collector_kind TEXT NOT NULL,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'observed',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX fact_records_scope_generation_idx
    ON fact_records (scope_id, generation_id, observed_at, fact_id);

INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, collector_kind, partition_key, status, active_generation_id)
VALUES
    ('scope-default', 'repository', 'git', 'git', 'repository:owner', 'active', 'gen-default'),
    ('scope-ref', 'repository_ref', 'git', 'git', 'repository:owner', 'active', 'gen-ref'),
    ('scope-foreign', 'repository', 'git', 'git', 'repository:foreign', 'active', 'gen-foreign'),
    ('scope-inactive', 'repository', 'git', 'git', 'repository:owner', 'failed', 'gen-inactive'),
    ('scope-failed-generation', 'repository', 'git', 'git', 'repository:owner', 'active', 'gen-failed');
INSERT INTO scope_generations (generation_id, scope_id, status)
VALUES
    ('gen-default', 'scope-default', 'active'),
    ('gen-ref', 'scope-ref', 'active'),
    ('gen-foreign', 'scope-foreign', 'active'),
    ('gen-inactive', 'scope-inactive', 'active'),
    ('gen-failed', 'scope-failed-generation', 'failed');
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, is_tombstone, payload)
VALUES
    ('workflow-default', 'scope-default', 'gen-default', 'ci.workflow_image_evidence', 'stable-default', 'git', 'git', 'workflow-default', '2026-08-04T12:00:00Z', FALSE, '{"repository_id":"repository:owner"}'),
    ('workflow-tombstone', 'scope-default', 'gen-default', 'ci.workflow_image_evidence', 'stable-tombstone', 'git', 'git', 'workflow-tombstone', '2026-08-04T12:00:01Z', TRUE, '{"repository_id":"repository:owner"}'),
    ('workflow-ref', 'scope-ref', 'gen-ref', 'ci.workflow_image_evidence', 'stable-ref', 'git', 'git', 'workflow-ref', '2026-08-04T12:00:02Z', FALSE, '{"repository_id":"repository:owner"}'),
    ('workflow-foreign', 'scope-foreign', 'gen-foreign', 'ci.workflow_image_evidence', 'stable-foreign', 'git', 'git', 'workflow-foreign', '2026-08-04T12:00:03Z', FALSE, '{"repository_id":"repository:foreign"}'),
    ('workflow-inactive', 'scope-inactive', 'gen-inactive', 'ci.workflow_image_evidence', 'stable-inactive', 'git', 'git', 'workflow-inactive', '2026-08-04T12:00:04Z', FALSE, '{"repository_id":"repository:owner"}'),
    ('workflow-failed-generation', 'scope-failed-generation', 'gen-failed', 'ci.workflow_image_evidence', 'stable-failed', 'git', 'git', 'workflow-failed', '2026-08-04T12:00:05Z', FALSE, '{"repository_id":"repository:owner"}');
`
