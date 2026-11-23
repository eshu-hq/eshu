// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Real-Postgres proof for issue #4968 (Sign-in policy, epic #4962): proves
// that postgresBrowserSessionAdapter.CreateBrowserSession — the single choke
// point every session-issuing path (local/break-glass, OIDC, SAML) funnels
// through — correctly captures the SSO-admin-proof guardrail signal only
// when BOTH AllScopes and ExternalProviderConfigID are set on the created
// session record, and never for a local/break-glass session (which never
// carries ExternalProviderConfigID). A regression here would silently make
// the require_sso guardrail impossible to satisfy (SSO admin logins never
// prove themselves) or, worse, satisfiable by a non-admin SSO login.
func TestCreateBrowserSessionRecordsSSOAdminVerificationOnlyForAdminExternalSessions(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the browser-session sign-in-policy wiring proof")
	}

	ctx := context.Background()
	db := openSignInPolicyWiringSchemaFixture(t, ctx, dsn)
	adapter := newPostgresBrowserSessionAdapter(db, nil)
	if adapter == nil {
		t.Fatal("newPostgresBrowserSessionAdapter() = nil")
	}

	now := time.Now().UTC()
	seedSignInPolicyWiringTenant(t, ctx, db, "tenant-admin-sso")
	seedSignInPolicyWiringTenant(t, ctx, db, "tenant-nonadmin-sso")
	seedSignInPolicyWiringTenant(t, ctx, db, "tenant-local-admin")

	// Admin session established via an external IdP: must record the proof.
	mustCreateSession(t, ctx, adapter, "tenant-admin-sso", true, "pc_admin_sso", now)
	assertSSOAdminVerified(t, ctx, db, "tenant-admin-sso", true)

	// Non-admin session established via an external IdP: must NOT record the
	// proof — a non-admin SSO login can never satisfy the guardrail.
	mustCreateSession(t, ctx, adapter, "tenant-nonadmin-sso", false, "pc_nonadmin_sso", now)
	assertSSOAdminVerified(t, ctx, db, "tenant-nonadmin-sso", false)

	// Admin session with NO external provider (local/break-glass login):
	// must NOT record the proof — only an actual SSO login counts.
	mustCreateSession(t, ctx, adapter, "tenant-local-admin", true, "", now)
	assertSSOAdminVerified(t, ctx, db, "tenant-local-admin", false)
}

func mustCreateSession(
	t *testing.T,
	ctx context.Context,
	adapter *postgresBrowserSessionAdapter,
	tenantID string,
	allScopes bool,
	externalProviderConfigID string,
	now time.Time,
) {
	t.Helper()
	record := query.BrowserSessionCreateRecord{
		SessionHash:        "sha256:session-" + tenantID,
		CSRFTokenHash:      "sha256:csrf-" + tenantID,
		TenantID:           tenantID,
		WorkspaceID:        "workspace-" + tenantID,
		SubjectIDHash:      "sha256:subject-" + tenantID,
		SubjectClass:       "local_user",
		PolicyRevisionHash: "sha256:policy",
		AllScopes:          allScopes,
		IssuedAt:           now,
		LastSeenAt:         now,
		IdleExpiresAt:      now.Add(30 * time.Minute),
		AbsoluteExpiresAt:  now.Add(12 * time.Hour),
		UpdatedAt:          now,
	}
	if externalProviderConfigID != "" {
		// The postgres store requires provider/subject-hash/validated-at/
		// stale-after to be set together (or all empty) for an external-auth
		// session; only a local/break-glass session leaves all four empty.
		record.ExternalProviderConfigID = externalProviderConfigID
		record.ExternalSubjectIDHash = "sha256:external-subject-" + tenantID
		record.ExternalGroupHashes = []string{"sha256:external-group-" + tenantID}
		record.ExternalAuthValidatedAt = now
		record.ExternalAuthStaleAfter = now.Add(15 * time.Minute)
	}
	if err := adapter.CreateBrowserSession(ctx, record); err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
}

func assertSSOAdminVerified(t *testing.T, ctx context.Context, db *sql.DB, tenantID string, want bool) {
	t.Helper()
	var verifiedAt sql.NullTime
	row := db.QueryRowContext(ctx, `
SELECT sso_admin_verified_at FROM identity_sign_in_policies WHERE tenant_id = $1
`, tenantID)
	if err := row.Scan(&verifiedAt); err != nil {
		if err == sql.ErrNoRows {
			if want {
				t.Fatalf("tenant %s: no sign-in policy row, want sso_admin_verified_at set", tenantID)
			}
			return
		}
		t.Fatalf("read sign-in policy row for %s: %v", tenantID, err)
	}
	if verifiedAt.Valid != want {
		t.Fatalf("tenant %s: sso_admin_verified_at set = %t, want %t", tenantID, verifiedAt.Valid, want)
	}
}

func seedSignInPolicyWiringTenant(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'active', '', 'sha256:policy', $2, $2, NULL)
ON CONFLICT (tenant_id) DO NOTHING
`, tenantID, now); err != nil {
		t.Fatalf("seed tenant %s: %v", tenantID, err)
	}
	workspaceID := "workspace-" + tenantID
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, $2, 'active', '', 'sha256:policy', $3, $3, NULL)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING
`, tenantID, workspaceID, now); err != nil {
		t.Fatalf("seed workspace for %s: %v", tenantID, err)
	}
}

func openSignInPolicyWiringSchemaFixture(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("sign_in_policy_wiring_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, stmt := range []string{
		pgstatus.MigrationSQL("ingestion_scopes"),
		pgstatus.MigrationSQL("tenant_workspace_grants"),
		pgstatus.MigrationSQL("identity_subjects"),
		pgstatus.MigrationSQL("browser_sessions"),
		pgstatus.MigrationSQL("identity_sign_in_policy"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply fixture schema: %v", err)
		}
	}
	return db
}
