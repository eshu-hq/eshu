// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Throwaway-schema bootstrap and fixture/seed helpers for
// TestReadRepositoryFreshnessLiveDB (repository_freshness_db_integration_test.go,
// issue #5148). Split into this file to stay under the repository's 500-line
// file cap; see the sibling file for the DSN gate and the test's rationale.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openRepositoryFreshnessDBIntegrationSchema creates an isolated throwaway
// schema, applies the ingestion_scopes/scope_generations/fact_records/
// fact_work_items/shared_projection_intents/webhook_refresh_triggers DDL, and
// returns a single-connection handle pinned to that schema. Mirrors
// openFactCrossBatchFencingSchema (facts_cross_batch_fencing_proof_test.go)
// and openReducerFairnessDBWithSchema (reducer_queue_domain_fairness_test.go)
// so this proof follows the package's established live-Postgres pattern.
func openRepositoryFreshnessDBIntegrationSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("repo_freshness_dbit_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v, want nil", err)
	}

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create repository freshness schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		MigrationSQL("fact_records"),
		MigrationSQL("fact_work_items"),
		MigrationSQL("shared_projection_intents"),
		MigrationSQL("webhook_refresh_triggers"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply repository freshness schema: %v", err)
		}
	}
	return db
}

// freshnessScopeFixture is one seeded ingestion_scopes/scope_generations/
// fact_records(repository) triple: the minimal live-Postgres row set
// resolveScope, readGeneration, readStageCounts, readSharedPending, and
// readUnobservedPush all read against.
type freshnessScopeFixture struct {
	scopeID        string
	generationID   string
	repoID         string // fact_records.payload->>'repo_id', the canonical id callers query by
	repoSlug       string // ingestion_scopes.payload->>'repo_slug', drives repo_display for the webhook lookup
	observedCommit string
	now            time.Time
}

func seedRepositoryFreshnessScope(t *testing.T, ctx context.Context, db *sql.DB, fx freshnessScopeFixture) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ($1, 'repo', 'git', $1, 'git', $1, $2, $2, 'active', $3, jsonb_build_object('repo_slug', $4::text))`,
		fx.scopeID, fx.now, fx.generationID, fx.repoSlug); err != nil {
		t.Fatalf("insert ingestion_scopes %s: %v", fx.scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
    observed_at, ingested_at, status, activated_at, payload
) VALUES ($1, $2, 'push', $3, false, $4, $4, 'active', $4, '{}'::jsonb)`,
		fx.generationID, fx.scopeID, fx.observedCommit, fx.now); err != nil {
		t.Fatalf("insert scope_generations %s: %v", fx.generationID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ('fact-'||$1, $1, $2, 'repository', 'fact-'||$1, 'git', 'fact-'||$1, $3, $3, jsonb_build_object('repo_id', $4::text))`,
		fx.scopeID, fx.generationID, fx.now, fx.repoID); err != nil {
		t.Fatalf("insert fact_records repository fact for %s: %v", fx.scopeID, err)
	}
}

func seedRepositoryFreshnessWorkItem(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, workItemID, stage, status string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    payload, created_at, updated_at
) VALUES ($1, $2, $3, $4, $4, $5, '{}'::jsonb, $6, $6)`,
		workItemID, scopeID, generationID, stage, status, now); err != nil {
		t.Fatalf("insert fact_work_items %s: %v", workItemID, err)
	}
}

func seedRepositoryFreshnessSharedIntent(t *testing.T, ctx context.Context, db *sql.DB, intentID, domain, repositoryID, generationID string, now time.Time, completedAt *time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, repository_id,
    source_run_id, generation_id, payload, created_at, completed_at
) VALUES ($1, $2, $1, $3, 'run-1', $4, '{}'::jsonb, $5, $6)`,
		intentID, domain, repositoryID, generationID, now, completedAt); err != nil {
		t.Fatalf("insert shared_projection_intents %s: %v", intentID, err)
	}
}
