// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

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

// TestIngestionScopesActiveStateSnapshotIndexAppliesReappliesAndRecoversLive
// is the migration-087 concurrency proof (issue #5593 P1 fix): first
// application, identical reapplication, a connection restart, and recovery
// from a same-name invalid index -- the proof ladder's "Concurrency" rung
// for index candidates, mirroring
// TestCloudResourceOwnerPageIndexesApplyAndReapplyLive's shape.
func TestIngestionScopesActiveStateSnapshotIndexAppliesReappliesAndRecoversLive(t *testing.T) {
	const schema = "eshu_5593_state_snapshot_catchup_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live ingestion_scopes catch-up index proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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

	// Seed a populated, non-empty ingestion_scopes table (repository rows the
	// dominant kind, a minority of state_snapshot rows, some without an
	// active generation) so CONCURRENTLY builds against real data, not an
	// empty table.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'git-repository-scope:repo-' || i, 'repository', 'git', 'repo-' || i, NULL, 'git', 'p' || (i % 8), now(), now(), 'active',
       CASE WHEN i % 3 = 0 THEN 'gen-repo-' || i ELSE NULL END, '{}'::jsonb
FROM generate_series(1, 9000) AS i;
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'state_snapshot:s3:' || md5('locator-' || i), 'state_snapshot', 'collector-terraform-state', 'k' || i, NULL, 'terraform-state', 'p' || (i % 8), now(), now(), 'active',
       CASE WHEN i % 10 != 0 THEN 'terraform_state:state_snapshot:s3:' || md5('locator-' || i) || ':lineage-' || i || ':serial:1' ELSE NULL END, '{}'::jsonb
FROM generate_series(1, 1000) AS i;
`); err != nil {
		t.Fatalf("seed populated ingestion_scopes: %v", err)
	}

	definitions := ingestionScopesActiveStateSnapshotIndexDefinitions(t)
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply ingestion_scopes catch-up index pass %d: %v", pass, err)
		}
	}
	assertIndexValidAndReady(ctx, t, db, schema, "ingestion_scopes_active_state_snapshot_idx", true)

	// Restart: a fresh connection re-running bootstrap must be a true no-op.
	restartDB, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open restart Postgres connection: %v", err)
	}
	restartDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = restartDB.Close() })
	if err := ApplyDefinitions(ctx, SQLDB{DB: restartDB}, definitions); err != nil {
		t.Fatalf("reapply ingestion_scopes catch-up index after connection restart: %v", err)
	}
	assertIndexValidAndReady(ctx, t, restartDB, schema, "ingestion_scopes_active_state_snapshot_idx", true)

	// Rollback: drop it, confirm gone.
	if _, err := restartDB.ExecContext(
		ctx,
		"DROP INDEX CONCURRENTLY IF EXISTS ingestion_scopes_active_state_snapshot_idx",
	); err != nil {
		t.Fatalf("rollback catch-up index: %v", err)
	}
	assertIndexValidAndReady(ctx, t, restartDB, schema, "ingestion_scopes_active_state_snapshot_idx", false)

	// Recovery: a same-name index left INVALID by a failed concurrent build
	// (simulated here by a duplicate-predicate unique index that fails on
	// conflicting keys) must be cleaned up and rebuilt by the next bootstrap
	// apply, not left permanently broken.
	if _, err := restartDB.ExecContext(ctx, `
CREATE UNIQUE INDEX CONCURRENTLY ingestion_scopes_active_state_snapshot_idx
    ON ingestion_scopes (scope_kind)
    WHERE scope_kind = 'state_snapshot' AND active_generation_id IS NOT NULL
`); err == nil {
		t.Fatal("seed invalid same-name catch-up index error = nil, want duplicate-key failure (multiple state_snapshot rows share scope_kind)")
	}
	if valid := ingestionScopesIndexValidity(t, ctx, restartDB, schema, "ingestion_scopes_active_state_snapshot_idx"); valid {
		t.Fatal("failed same-name catch-up index is valid, want invalid")
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: restartDB}, definitions); err != nil {
		t.Fatalf("recover invalid catch-up index through bootstrap definition: %v", err)
	}
	assertIndexValidAndReady(ctx, t, restartDB, schema, "ingestion_scopes_active_state_snapshot_idx", true)

	// The index must actually be USED by the lister's exact query shape, not
	// merely present -- the whole point of this migration.
	var planText string
	rows, err := restartDB.QueryContext(ctx, `
EXPLAIN SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_kind = 'state_snapshot'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT 500
`)
	if err != nil {
		t.Fatalf("explain lister query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	planText = strings.Join(lines, "\n")
	if !strings.Contains(planText, "ingestion_scopes_active_state_snapshot_idx") {
		t.Fatalf("lister query plan does not use the catch-up index:\n%s", planText)
	}
}

func ingestionScopesIndexValidity(t *testing.T, ctx context.Context, db *sql.DB, schema, indexName string) bool {
	t.Helper()
	var valid bool
	if err := db.QueryRowContext(ctx, `
SELECT i.indisvalid
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relname = $2
`, schema, indexName).Scan(&valid); err != nil {
		t.Fatalf("query index validity for %s.%s: %v", schema, indexName, err)
	}
	return valid
}

func ingestionScopesActiveStateSnapshotIndexDefinitions(t *testing.T) []Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "ingestion_scopes_active_state_snapshot_index" {
			return []Definition{definition}
		}
	}
	t.Fatal("ingestion_scopes_active_state_snapshot_index definition not found")
	return nil
}
