// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// generationRetentionMigratedSchemaRequiredEnv turns a missing DSN into a
// failure. CI lanes that enroll these proofs set it to "1".
const generationRetentionMigratedSchemaRequiredEnv = "ESHU_REQUIRE_RETENTION_MIGRATED_SCHEMA_PROOF"

// generationRetentionMigratedSchemaStatements lists every SQL statement owned
// by generation_retention_sql.go. Each one is prepared against the migrated
// schema so a table or column that drifts from the migrations fails here
// instead of at the first prunable retention batch (#6809).
var generationRetentionMigratedSchemaStatements = map[string]string{
	"candidate":                        generationRetentionCandidateQuery,
	"row_counts":                       generationRetentionRowCountsQuery,
	"insert_event":                     insertGenerationRetentionEventQuery,
	"delete_shared_projection_intents": deleteSharedProjectionIntentsForGenerationsQuery,
	"delete_unroutable_intents":        deleteSharedProjectionUnroutableIntentsForGenerationsQuery,
	"prune_content_file_references":    pruneContentFileReferencesForGenerationsQuery,
	"prune_content_entities":           pruneContentEntitiesForGenerationsQuery,
	"prune_content_files":              pruneContentFilesForGenerationsQuery,
	"delete_scope_generations":         deleteScopeGenerationsForRetentionQuery,
}

// openGenerationRetentionMigratedSchema applies the real bootstrap migrations
// to an isolated schema of the database named by ESHU_POSTGRES_TEST_DSN or
// ESHU_POSTGRES_DSN, or skips. It never uses a hand-written schema, so the retention SQL is proven
// against the tables and columns production actually has.
func openGenerationRetentionMigratedSchema(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		// The reducer contention gate sets the require flag so a renamed DSN
		// variable fails the lane instead of skipping the proof there.
		if os.Getenv(generationRetentionMigratedSchemaRequiredEnv) == "1" {
			t.Fatalf("%s=1 but neither ESHU_POSTGRES_TEST_DSN nor ESHU_POSTGRES_DSN is set", generationRetentionMigratedSchemaRequiredEnv)
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the migrated-schema retention proof")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	schema := "eshu_6809_retention_migrated"
	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsed.Query()
	// public stays on the path so a pg_trgm extension a sibling live test
	// installed there still resolves its operator classes; every table the
	// bootstrap creates lands in the isolated schema, which comes first.
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap migrations: %v", err)
	}
	return database, ctx
}

// TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive fails when
// any retention statement names a relation or column the migrations do not
// create.
func TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for name, statement := range generationRetentionMigratedSchemaStatements {
		t.Run(name, func(t *testing.T) {
			prepared, err := database.PrepareContext(ctx, statement)
			if err != nil {
				t.Fatalf("prepare %s against migrated schema: %v", name, err)
			}
			_ = prepared.Close()
		})
	}
}

// TestGenerationRetentionPrunesMigratedSchemaLive seeds one prunable
// superseded generation with rows in every counted table that the migrations
// let a fixture populate, then proves the row-count query reports exactly those
// rows and the full prune removes them, on the real migrated schema.
func TestGenerationRetentionPrunesMigratedSchemaLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedGenerationRetentionMigratedFixture(t, ctx, database)

	want := map[string]int64{
		"fact_records":                        2,
		"fact_work_items":                     1,
		"fact_replay_events":                  1,
		"semantic_extraction_jobs":            0,
		"shared_projection_acceptance":        1,
		"graph_projection_phase_state":        0,
		"graph_projection_phase_repair_queue": 0,
		"iac_reachability_rows":               2,
		"shared_projection_intents":           1,
		"content_file_references":             2,
		"content_entities":                    1,
		"infra_resource_entities":             1,
		"content_files":                       1,
	}

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	totals, _, _, err := GenerationRetentionStore{}.countRows(ctx, tx, []string{"gen-old"})
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("countRows() error = %v", err)
	}
	for table, wantCount := range want {
		if got := totals[table]; got != wantCount {
			t.Errorf("row count for %s = %d, want %d", table, got, wantCount)
		}
	}
	if len(totals) != len(want) {
		t.Errorf("countRows() reported %d tables, want %d: %v", len(totals), len(want), totals)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "6809-migrated-schema",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	for _, table := range []string{"fact_records", "content_file_references", "content_entities", "infra_resource_entities", "content_files", "shared_projection_intents"} {
		if got := result.RowsPruned[table]; got != want[table] {
			t.Errorf("RowsPruned[%s] = %d, want %d", table, got, want[table])
		}
	}
	for _, table := range []string{"fact_records", "iac_reachability_rows", "content_file_references", "content_entities", "content_files", "infra_resource_entities"} {
		var remaining int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&remaining); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if remaining != 0 {
			t.Errorf("%s has %d rows after prune, want 0", table, remaining)
		}
	}
}

// seedGenerationRetentionMigratedFixture inserts one active generation and one
// prunable superseded generation ("gen-old") with dependent rows.
func seedGenerationRetentionMigratedFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-1', 'repository', 'git', 'repo-1', 'git', 'repo-1', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-active', 'scope-1', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-active' WHERE scope_id = 'scope-1'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, superseded_at)
VALUES ('gen-old', 'scope-1', 'snapshot', now() - interval '2 days', now() - interval '2 days',
    'superseded', now() - interval '1 day')`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload) VALUES
('fact-file', 'scope-1', 'gen-old', 'file', 'k-file', 'git', 'k-file', now(), now(),
    '{"repo_id":"repo-1","relative_path":"main.tf"}'::jsonb),
('fact-entity', 'scope-1', 'gen-old', 'content_entity', 'k-entity', 'git', 'k-entity', now(), now(),
    '{"repo_id":"repo-1","entity_id":"entity-1"}'::jsonb)`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ('work-1', 'scope-1', 'gen-old', 'reducer', 'domain', 'succeeded', now(), now())`,
		`INSERT INTO fact_replay_events (replay_event_id, work_item_id, scope_id, generation_id, created_at)
VALUES ('replay-1', 'work-1', 'scope-1', 'gen-old', now())`,
		`INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
VALUES ('scope-1', 'unit-1', 'run-1', 'gen-old', now(), now())`,
		`INSERT INTO iac_reachability_rows (scope_id, generation_id, repo_id, family, artifact_path, artifact_name,
    reachability, finding, confidence, evidence, limitations, observed_at, updated_at) VALUES
('scope-1', 'gen-old', 'repo-1', 'terraform', 'a', 'a', 'used', 'f', 1, '[]'::jsonb, '[]'::jsonb, now(), now()),
('scope-1', 'gen-old', 'repo-1', 'terraform', 'b', 'b', 'unused', 'f', 1, '[]'::jsonb, '[]'::jsonb, now(), now())`,
		`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at)
VALUES ('intent-1', 'domain', 'p', 'scope-1', 'unit-1', 'repo-1', 'run-1', 'gen-old', '{}'::jsonb, now())`,
		`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-1', 'main.tf', 'x', 'h', 1, now())`,
		`INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at) VALUES
('repo-1', 'main.tf', 'module', 'a', now()),
('repo-1', 'main.tf', 'module', 'b', now())`,
		`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ('entity-1', 'repo-1', 'main.tf', 'TerraformResource', 'r', 1, 2, 'x', now())`,
		`INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
VALUES ('entity-1', 'repo-1', 'main.tf', 'TerraformResource', 'r', now())`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}
