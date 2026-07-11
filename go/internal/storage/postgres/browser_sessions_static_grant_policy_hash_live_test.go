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

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestBrowserSessionStaticGrantPolicyHashResolvesAgainstLiveWorkspaceHash is
// the real-Postgres end-to-end proof for issue #5038: a static/env OIDC
// provider (ESHU_AUTH_OIDC_CONFIG_FILE) whose role_grants[].policy_revision_hash
// does not match the live workspace policy revision hash must not produce a
// session that silently 401s every subsequent authenticated request even
// though login itself succeeded.
//
// The fix is in go/internal/oidclogin (StaticGrantResolver.ResolveGroupGrants
// never propagates the operator-supplied hash — see static_grants_test.go's
// TestStaticGrantResolverNeverPopulatesPolicyRevisionHash for that RED/GREEN
// unit proof), which relies entirely on the session-create contract this test
// exercises directly against real Postgres:
// createBrowserSessionQuery's COALESCE(NULLIF($7, empty string), ws.policy_revision_hash)
// (browser_sessions_schema.go) defaults an empty supplied hash to the live
// workspace hash, and resolveBrowserSessionQuery's join
// (sess.policy_revision_hash = ws.policy_revision_hash) is the exact
// mechanism that silently excludes — and therefore 401s — a session whose
// stored hash drifted from the live one.
//
// This SQL contract is not changed by #5038 (the DB-backed group-mapping path
// already relied on it), so there is no RED/GREEN against a diff in this
// package; instead this test is a permanent end-to-end regression guard
// proving both halves of the contract the Go-layer fix depends on:
//
//   - a session created with a WRONG, non-empty hash (what the pre-#5038
//     StaticGrantResolver produced from an operator's hand-set config value)
//     is written verbatim and EXCLUDED by the join — this reproduces the
//     reported bug's exact symptom.
//   - a session created with an EMPTY hash (what the fixed StaticGrantResolver
//     always produces now) is defaulted to the live workspace hash at insert
//     and RESOLVES.
//
// Run with: ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu
//
//	go test ./internal/storage/postgres -run 'StaticGrantPolicyHash' -count=1
func TestBrowserSessionStaticGrantPolicyHashResolvesAgainstLiveWorkspaceHash(t *testing.T) {
	dsn := staticGrantPolicyHashLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres static-grant policy-hash proof")
	}

	ctx := context.Background()
	schemaDB := openStaticGrantPolicyHashLiveSchema(t, ctx, dsn)
	liveHash := seedStaticGrantPolicyHashLiveWorkspace(t, ctx, schemaDB, "tenant_dsn", "workspace_dsn")

	store := NewBrowserSessionStore(SQLDB{DB: schemaDB})
	now := time.Now().UTC()

	t.Run("wrong operator-supplied hash is excluded (reproduces the #5038 silent-401 symptom)", func(t *testing.T) {
		sessionHash := "sha256:wrong-hash-session"
		if err := store.CreateSession(ctx, BrowserSessionRecord{
			SessionHash:   sessionHash,
			CSRFTokenHash: "sha256:csrf-wrong",
			TenantID:      "tenant_dsn",
			WorkspaceID:   "workspace_dsn",
			SubjectIDHash: "sha256:subject-wrong",
			SubjectClass:  "external_oidc_user",
			// This is what the pre-#5038 StaticGrantResolver propagated from
			// an operator's hand-set (and here, deliberately wrong) config
			// value.
			PolicyRevisionHash:       "sha256:operator-supplied-wrong-hash",
			RoleIDs:                  []string{"developer"},
			AllowedScopeIDs:          []string{"scope_a"},
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject-wrong",
			ExternalGroupHashes:      []string{"sha256:group"},
			ExternalAuthValidatedAt:  now,
			ExternalAuthStaleAfter:   now.Add(15 * time.Minute),
			IssuedAt:                 now,
			LastSeenAt:               now,
			IdleExpiresAt:            now.Add(30 * time.Minute),
			AbsoluteExpiresAt:        now.Add(12 * time.Hour),
			UpdatedAt:                now,
		}); err != nil {
			t.Fatalf("CreateSession() (wrong hash) error = %v", err)
		}

		_, ok, err := store.ResolveSessionHash(
			ctx, sessionHash, "sha256:csrf-wrong", false, now.Add(time.Minute), 30*time.Minute,
		)
		if err != nil {
			t.Fatalf("ResolveSessionHash() (wrong hash) error = %v", err)
		}
		if ok {
			t.Fatal("ResolveSessionHash() (wrong hash) ok = true, want false — " +
				"a session whose stored policy_revision_hash drifted from the live workspace hash must not resolve")
		}
	})

	t.Run("empty policy revision hash defaults to the live workspace hash and resolves", func(t *testing.T) {
		sessionHash := "sha256:fixed-hash-session"
		if err := store.CreateSession(ctx, BrowserSessionRecord{
			SessionHash:   sessionHash,
			CSRFTokenHash: "sha256:csrf-fixed",
			TenantID:      "tenant_dsn",
			WorkspaceID:   "workspace_dsn",
			SubjectIDHash: "sha256:subject-fixed",
			SubjectClass:  "external_oidc_user",
			// The fixed StaticGrantResolver never populates this, regardless
			// of what the operator's config file sets.
			PolicyRevisionHash:       "",
			RoleIDs:                  []string{"developer"},
			AllowedScopeIDs:          []string{"scope_a"},
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject-fixed",
			ExternalGroupHashes:      []string{"sha256:group"},
			ExternalAuthValidatedAt:  now,
			ExternalAuthStaleAfter:   now.Add(15 * time.Minute),
			IssuedAt:                 now,
			LastSeenAt:               now,
			IdleExpiresAt:            now.Add(30 * time.Minute),
			AbsoluteExpiresAt:        now.Add(12 * time.Hour),
			UpdatedAt:                now,
		}); err != nil {
			t.Fatalf("CreateSession() (empty hash) error = %v", err)
		}

		record, ok, err := store.ResolveSessionHash(
			ctx, sessionHash, "sha256:csrf-fixed", false, now.Add(time.Minute), 30*time.Minute,
		)
		if err != nil {
			t.Fatalf("ResolveSessionHash() (empty hash) error = %v", err)
		}
		if !ok {
			t.Fatal("ResolveSessionHash() (empty hash) ok = false, want true — " +
				"an empty supplied hash must default to the live workspace hash and resolve")
		}
		if record.PolicyRevisionHash != liveHash {
			t.Fatalf("resolved policy_revision_hash = %q, want live workspace hash %q", record.PolicyRevisionHash, liveHash)
		}
	})
}

func staticGrantPolicyHashLiveProofDSN() string {
	return os.Getenv("ESHU_POSTGRES_DSN")
}

// openStaticGrantPolicyHashLiveSchema creates an isolated throwaway schema and
// applies exactly the migrations browser_sessions depends on: ingestion_scopes
// (an FK target inside tenant_workspace_grants) and tenant_workspace_grants
// (tenants and workspaces, browser_sessions' own FK target), mirroring
// openProviderConfigLiveSchema (identity_provider_config_live_test.go) —
// this package's established live-Postgres proof pattern.
func openStaticGrantPolicyHashLiveSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("static_grant_policy_hash_live_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create static grant policy hash live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, defName := range []string{"ingestion_scopes", "tenant_workspace_grants", "browser_sessions"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(defName)); err != nil {
			t.Fatalf("apply migration %q: %v", defName, err)
		}
	}
	return db
}

// seedStaticGrantPolicyHashLiveWorkspace seeds an active tenant and workspace
// with a distinct, realistic-looking policy_revision_hash and returns the
// workspace's hash so the test can assert a resolved session bound to it.
func seedStaticGrantPolicyHashLiveWorkspace(
	t *testing.T, ctx context.Context, db *sql.DB, tenantID, workspaceID string,
) string {
	t.Helper()
	now := time.Now().UTC()
	liveHash := "sha256:live-workspace-hash-" + workspaceID
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, policy_revision_hash, created_at, updated_at)
VALUES ($1, 'active', $2, $3, $3)
ON CONFLICT (tenant_id) DO NOTHING`, tenantID, "sha256:live-tenant-hash-"+tenantID, now); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, policy_revision_hash, created_at, updated_at)
VALUES ($1, $2, 'active', $3, $4, $4)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING`, tenantID, workspaceID, liveHash, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	return liveHash
}
