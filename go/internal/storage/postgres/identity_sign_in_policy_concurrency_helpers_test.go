// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Shared DSN/schema fixture helpers for the sign-in-policy real-Postgres
// concurrency gate (identity_sign_in_policy_concurrency_test.go). Split into
// this sibling file to keep that file under the repository's 500-line cap;
// same package, so TestSignInPolicyConcurrencyGate's subtests keep calling
// these directly.

func signInPolicyProofDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_SIGN_IN_POLICY_PROOF_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func openSignInPolicySchemaFixture(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("sign_in_policy_%d", time.Now().UnixNano())
	db := openSignInPolicyConnRaw(t, dsn)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create sign-in policy schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("tenant_workspace_grants"),
		MigrationSQL("identity_subjects"),
		MigrationSQL("identity_sign_in_policy"),
		MigrationSQL("browser_sessions"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply sign-in policy fixture schema: %v", err)
		}
	}
	return db, schemaName
}

func openSignInPolicyConnRaw(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// openSignInPolicyConn opens an independent single-connection handle bound to
// an already-created fixture schema, so concurrent goroutines each hold their
// own live Postgres connection and their statements truly interleave at the
// database rather than serializing behind one pooled connection.
func openSignInPolicyConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	db := openSignInPolicyConnRaw(t, dsn)
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}

func seedSignInPolicyTenant(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'active', '', 'sha256:policy', $2, $2, NULL)
ON CONFLICT (tenant_id) DO NOTHING
`, tenantID, now); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}

func seedSignInPolicyWorkspace(t *testing.T, ctx context.Context, db *sql.DB, tenantID, workspaceID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, $2, 'active', '', 'sha256:policy', $3, $3, NULL)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING
`, tenantID, workspaceID, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
}

func insertActiveProviderConfig(t *testing.T, ctx context.Context, db *sql.DB, tenantID, providerConfigID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_provider_configs (
    provider_config_id, tenant_id, provider_kind, provider_key_hash, status,
    active_revision_id, created_at, updated_at
)
VALUES ($1, $2, 'oidc', $3, 'active', 'rev_seed', $4, $4)
`, providerConfigID, tenantID, "sha256:"+providerConfigID, now); err != nil {
		t.Fatalf("seed active provider config: %v", err)
	}
}
