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

// TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive proves
// the #5490 index (content_entities_k8s_select_partial_idx) applies cleanly
// to a populated store, reapplies as a no-op (CONCURRENTLY IF NOT EXISTS),
// and rolls back cleanly, per the eshu-performance-rigor concurrency proof
// requirement for index candidates: first application, identical
// reapplication, and rollback on an isolated populated store.
func TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive(t *testing.T) {
	const schema = "eshu_5490_k8s_select_index_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5490 K8sResource candidate index proof")
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

	// Seed the same worst-case shape as the #5363/#5490 evidence: repo-1 has
	// 6,000 K8sResource rows (3,000 Service + 3,000 Deployment) plus 4,000
	// non-K8sResource filler, so the populated store this index is applied to
	// matches the measured proof, not an empty table.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE content_entities (
    entity_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_byte INTEGER NULL,
    end_byte INTEGER NULL,
    language TEXT NULL,
    source_cache TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    indexed_at TIMESTAMPTZ NOT NULL,
    artifact_type TEXT NULL,
    template_dialect TEXT NULL,
    iac_relevant BOOLEAN NULL
);

INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, language, source_cache, metadata, indexed_at
)
SELECT 'repo-1-svc-' || i, 'repo-1', 'k8s/svc-' || i || '.yaml', 'K8sResource', 'svc-' || i,
       1, 20, 'yaml', 'kind: Service',
       jsonb_build_object('kind', 'Service', 'namespace', 'ns-' || (i % 10), 'selector', 'app=svc-' || i),
       now()
FROM generate_series(1, 3000) AS i;

INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, language, source_cache, metadata, indexed_at
)
SELECT 'repo-1-deploy-' || i, 'repo-1', 'k8s/deploy-' || i || '.yaml', 'K8sResource', 'deploy-' || i,
       1, 40, 'yaml', 'kind: Deployment',
       jsonb_build_object('kind', 'Deployment', 'namespace', 'ns-' || (i % 10), 'pod_template_labels', 'app=deploy-' || i),
       now()
FROM generate_series(1, 3000) AS i;

INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, language, source_cache, metadata, indexed_at
)
SELECT 'repo-1-fn-' || i, 'repo-1', 'src/file-' || i || '.go', 'Function', 'Func' || i,
       1, 10, 'go', 'func Func() {}', '{}'::jsonb, now()
FROM generate_series(1, 4000) AS i;
`); err != nil {
		t.Fatalf("seed populated content_entities worst-case partition: %v", err)
	}

	definitions := contentEntitiesK8sSelectPartialIndexDefinitions(t)

	// First application, then an identical reapplication: CONCURRENTLY IF NOT
	// EXISTS must be a clean no-op the second time, not an error or a rebuild.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply content_entities_k8s_select_partial_idx pass %d: %v", pass, err)
		}
	}

	assertIndexValidAndReady(ctx, t, db, schema, "content_entities_k8s_select_partial_idx", true)

	// Rollback: DROP INDEX CONCURRENTLY IF EXISTS must remove the index
	// cleanly from a populated, previously-indexed store.
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS content_entities_k8s_select_partial_idx"); err != nil {
		t.Fatalf("rollback content_entities_k8s_select_partial_idx: %v", err)
	}
	assertIndexValidAndReady(ctx, t, db, schema, "content_entities_k8s_select_partial_idx", false)

	// Re-apply once more after rollback to prove the migration is safe to
	// run again on a store that has already rolled the index back (the
	// restart/bootstrap-after-rollback case).
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("reapply content_entities_k8s_select_partial_idx after rollback: %v", err)
	}
	assertIndexValidAndReady(ctx, t, db, schema, "content_entities_k8s_select_partial_idx", true)
}

func contentEntitiesK8sSelectPartialIndexDefinitions(t *testing.T) []Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "content_entities_k8s_select_partial_index" {
			return []Definition{definition}
		}
	}
	t.Fatal("content_entities_k8s_select_partial_index definition not found")
	return nil
}

func assertIndexValidAndReady(ctx context.Context, t *testing.T, db *sql.DB, schema, indexName string, wantPresent bool) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2
`, schema, indexName)
	if err != nil {
		t.Fatalf("inspect index %s: %v", indexName, err)
	}
	defer func() { _ = rows.Close() }()

	present := false
	for rows.Next() {
		var valid, ready bool
		if err := rows.Scan(&valid, &ready); err != nil {
			t.Fatalf("scan index state %s: %v", indexName, err)
		}
		present = true
		if !valid || !ready {
			t.Errorf("index %s state valid=%t ready=%t, want true/true", indexName, valid, ready)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index state %s: %v", indexName, err)
	}
	if present != wantPresent {
		t.Fatalf("index %s present=%t, want %t", indexName, present, wantPresent)
	}
}
