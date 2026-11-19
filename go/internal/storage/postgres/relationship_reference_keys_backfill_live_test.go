// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLookupKeyBackfillRebuildsVersionedRowsAndKeys is the live proof for
// the #10875 rebuild: legacy facts with no sidecar rows get version-1
// relationship rows plus security-alert keys from the same derivation the
// save path uses, a rerun is a no-op, and the ready marker flips the keyed
// security reader on only when set.
func TestLookupKeyBackfillRebuildsVersionedRowsAndKeys(t *testing.T) {
	dsn := os.Getenv("ESHU_SECURITY_ALERT_KEYED_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SECURITY_ALERT_KEYED_PROOF_DSN to run the lookup-key backfill proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := fmt.Sprintf("lookup_key_backfill_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL, stable_fact_key TEXT NOT NULL DEFAULT '',
    schema_version TEXT NOT NULL DEFAULT '0', collector_kind TEXT NOT NULL DEFAULT 'u',
    fencing_token BIGINT NOT NULL DEFAULT 0, source_confidence TEXT NOT NULL DEFAULT 'u',
    source_system TEXT NOT NULL DEFAULT 'git', source_fact_key TEXT NOT NULL DEFAULT '',
    source_uri TEXT NULL, source_record_id TEXT NULL, observed_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE, payload JSONB NOT NULL DEFAULT '{}'::jsonb
)`,
		`CREATE TABLE relationship_reference_candidate_keys (
    fact_id TEXT NOT NULL, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
    source_repo_id TEXT NOT NULL, reference_key TEXT NOT NULL,
    keys_version SMALLINT NOT NULL DEFAULT 0, fact_kind TEXT NOT NULL DEFAULT '',
    artifact_type_norm TEXT NOT NULL DEFAULT '', path_norm TEXT NOT NULL DEFAULT '',
    content_argocd_kind BOOLEAN NOT NULL DEFAULT FALSE,
    content_salt_gitfs BOOLEAN NOT NULL DEFAULT FALSE,
    flux_git_nonempty BOOLEAN NOT NULL DEFAULT FALSE,
    um_kind_application BOOLEAN NOT NULL DEFAULT FALSE,
    um_artifact_argocd BOOLEAN NOT NULL DEFAULT FALSE,
    um_argocd_applications BOOLEAN NOT NULL DEFAULT FALSE,
    um_argocd_applicationsets BOOLEAN NOT NULL DEFAULT FALSE,
    um_flux_git_repositories BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (fact_id)
)`,
		`CREATE TABLE security_alert_reconciliation_keys (
    fact_id TEXT NOT NULL, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
    key_kind TEXT NOT NULL, key_value TEXT NOT NULL,
    PRIMARY KEY (fact_id, key_kind, key_value)
)`,
		`CREATE TABLE security_alert_reconciliation_key_backfill_markers (marker_name TEXT PRIMARY KEY, completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, observed_at, payload) VALUES
('fact-terraform', 'scope-1', 'gen-1', 'content', NOW(), '{"repo_id": "acme/app", "artifact_type": "terraform", "relative_path": "main.tf", "content": "module x { source = acme/lib }"}'),
('fact-argocd', 'scope-1', 'gen-1', 'content', NOW(), '{"repo_id": "acme/app", "artifact_type": "argocd", "content_path": "app.yaml", "content": "kind: Application"}'),
('fact-alert', 'scope-1', 'gen-1', 'security_alert.repository_alert', NOW(), '{"repository_id": "acme/app"}'),
('fact-alert-empty', 'scope-1', 'gen-1', 'security_alert.repository_alert', NOW(), '{}')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("proof ddl/seed: %v", err)
		}
	}
	store := SQLDB{DB: db}

	ready, err := SecurityAlertReconciliationKeysReady(ctx, store)
	if err != nil {
		t.Fatalf("readiness before backfill: %v", err)
	}
	if ready {
		t.Fatal("ready = true before the marker exists")
	}

	completed, err := LookupKeyBackfill{}.Run(ctx, store, 25, 500)
	if err != nil {
		t.Fatalf("backfill run: %v", err)
	}
	if !completed {
		t.Fatal("completed = false after a full single-scope run")
	}
	var versions []int
	rows, err := db.QueryContext(ctx, `SELECT keys_version FROM relationship_reference_candidate_keys ORDER BY fact_id`)
	if err != nil {
		t.Fatalf("read versions: %v", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, v)
	}
	_ = rows.Close()
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 1 {
		t.Fatalf("keys_version = %v, want two version-1 rows (content facts only)", versions)
	}
	var keyCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM security_alert_reconciliation_keys`).Scan(&keyCount); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("security keys = %d, want 1", keyCount)
	}
	missing, err := countLookupKeyMissingFacts(ctx, store, "scope-1")
	if err != nil {
		t.Fatalf("count missing after run: %v", err)
	}
	if missing != 1 {
		t.Fatalf("missing = %d, want 1 (the keyless security fact)", missing)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM security_alert_reconciliation_keys WHERE fact_id = 'fact-alert'`); err != nil {
		t.Fatalf("drop alert keys: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE relationship_reference_candidate_keys SET keys_version = 0 WHERE fact_id = 'fact-terraform'`); err != nil {
		t.Fatalf("downgrade version: %v", err)
	}
	missing, err = countLookupKeyMissingFacts(ctx, store, "scope-1")
	if err != nil {
		t.Fatalf("count missing after drops: %v", err)
	}
	if missing != 3 {
		t.Fatalf("missing = %d, want 3 (keyless alert, keyless fact-alert, version-0 terraform)", missing)
	}
	healed, err := LookupKeyBackfill{}.Run(ctx, store, 25, 500)
	if err != nil {
		t.Fatalf("heal run: %v", err)
	}
	if !healed {
		t.Fatal("heal run completed = false, want true")
	}
	missing, err = countLookupKeyMissingFacts(ctx, store, "scope-1")
	if err != nil {
		t.Fatalf("count missing after heal: %v", err)
	}
	if missing != 1 {
		t.Fatalf("missing = %d after heal, want 1 (only the keyless alert)", missing)
	}
	var artifact, path string
	var argocd bool
	if err := db.QueryRowContext(ctx, `SELECT artifact_type_norm, path_norm, content_argocd_kind FROM relationship_reference_candidate_keys WHERE fact_id = 'fact-argocd'`).Scan(&artifact, &path, &argocd); err != nil {
		t.Fatalf("read rebuilt columns: %v", err)
	}
	if artifact != "argocd" || path != "app.yaml" || !argocd {
		t.Fatalf("rebuilt columns = (%q, %q, %v), want (argocd, app.yaml, true)", artifact, path, argocd)
	}

	var relBefore, keysBefore int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM relationship_reference_candidate_keys`).Scan(&relBefore); err != nil {
		t.Fatalf("count rel before rerun: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM security_alert_reconciliation_keys`).Scan(&keysBefore); err != nil {
		t.Fatalf("count keys before rerun: %v", err)
	}
	completed, err = LookupKeyBackfill{}.Run(ctx, store, 25, 500)
	if err != nil {
		t.Fatalf("backfill rerun: %v", err)
	}
	if !completed {
		t.Fatal("rerun completed = false, want true (nothing missing)")
	}
	var relAfter, keysAfter int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM relationship_reference_candidate_keys`).Scan(&relAfter); err != nil {
		t.Fatalf("count rel after rerun: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM security_alert_reconciliation_keys`).Scan(&keysAfter); err != nil {
		t.Fatalf("count keys after rerun: %v", err)
	}
	if relBefore != relAfter || keysBefore != keysAfter {
		t.Fatalf("rerun changed row counts rel %d->%d keys %d->%d, want identical", relBefore, relAfter, keysBefore, keysAfter)
	}

	if err := MarkSecurityAlertReconciliationKeysReady(ctx, store); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	ready, err = SecurityAlertReconciliationKeysReady(ctx, store)
	if err != nil {
		t.Fatalf("readiness after marker: %v", err)
	}
	if !ready {
		t.Fatal("ready = false after the marker exists")
	}
}
