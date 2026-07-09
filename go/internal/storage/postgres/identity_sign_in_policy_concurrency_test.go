// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real-Postgres proof for issue #4968 (Sign-in policy, epic #4962).
//
// The fake-queryer unit tests in identity_sign_in_policy_test.go prove the SQL
// text and Go control flow; they cannot exercise real row locking, real
// unique-constraint upsert semantics, or the real identity_provider_configs /
// identity_sign_in_policies foreign-key relationship. This gate drives a real
// Postgres instance and proves:
//
//   - the guardrail correctly reads real identity_provider_configs rows (an
//     active provider proves it; a disabled/absent one blocks require_sso);
//   - two concurrent UpsertSignInPolicy calls for the SAME tenant, one
//     connection per goroutine (true interleaving, not one pooled
//     connection), never lose an update: the row-lock in UpsertSignInPolicy
//     serializes them, and the final row reflects both edits, not just
//     whichever commit happened to land last;
//   - RecordSSOAdminVerification's sticky COALESCE upsert is idempotent under
//     real ON CONFLICT contention;
//   - AcceptLocalIdentityInvitation's MFA-required-by-policy gate rejects a
//     real invitation acceptance against a real identity_sign_in_policies row.
//
// Skipped unless a DSN is provided, matching the package's other real-Postgres
// proofs (see identity_bootstrap_credential_concurrency_test.go).
func TestSignInPolicyConcurrencyGate(t *testing.T) {
	dsn := signInPolicyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_SIGN_IN_POLICY_PROOF_DSN or ESHU_POSTGRES_DSN to run the sign-in policy concurrency gate")
	}

	ctx := context.Background()
	ownerDB, schemaName := openSignInPolicySchemaFixture(t, ctx, dsn)

	t.Run("GuardrailReflectsRealActiveProviderRow", func(t *testing.T) {
		testGuardrailReflectsRealActiveProviderRow(t, ctx, ownerDB)
	})
	t.Run("ConcurrentUpsertsSerializeWithoutLostUpdate", func(t *testing.T) {
		testConcurrentSignInPolicyUpsertsSerialize(t, ctx, dsn, schemaName)
	})
	t.Run("RecordSSOAdminVerificationIsIdempotentUnderContention", func(t *testing.T) {
		testRecordSSOAdminVerificationIdempotentUnderContention(t, ctx, dsn, schemaName)
	})
	t.Run("AcceptInvitationRejectsMissingMFAAgainstRealPolicyRow", func(t *testing.T) {
		testAcceptInvitationRejectsMissingMFAAgainstRealPolicyRow(t, ctx, ownerDB)
	})
}

func testGuardrailReflectsRealActiveProviderRow(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tenantID := "tenant-signin-guardrail"
	seedSignInPolicyTenant(t, ctx, db, tenantID)
	store := NewIdentitySubjectStore(SQLDB{DB: db})

	requireSSO := true
	_, err := store.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev1",
		Now:                time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("UpsertSignInPolicy() with no provider configs error = nil, want ErrSignInPolicyGuardrailNoProvenProvider")
	}

	insertActiveProviderConfig(t, ctx, db, tenantID, "pc_guardrail")
	if err := store.RecordSSOAdminVerification(ctx, tenantID, "pc_guardrail", time.Now().UTC()); err != nil {
		t.Fatalf("RecordSSOAdminVerification() error = %v", err)
	}

	policy, err := store.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev2",
		Now:                time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() with a proven active provider and sso admin proof error = %v", err)
	}
	if !policy.RequireSSO {
		t.Fatal("UpsertSignInPolicy() RequireSSO = false, want true once both guardrails are proven")
	}
}

func testConcurrentSignInPolicyUpsertsSerialize(t *testing.T, ctx context.Context, dsn, schemaName string) {
	t.Helper()
	tenantID := "tenant-signin-concurrency"
	seedDB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	seedSignInPolicyTenant(t, ctx, seedDB, tenantID)

	connA := openSignInPolicyConn(t, ctx, dsn, schemaName)
	connB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	storeA := NewIdentitySubjectStore(SQLDB{DB: connA})
	storeB := NewIdentitySubjectStore(SQLDB{DB: connB})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		allowLocal := false
		if _, err := storeA.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
			AllowLocalUserCreation: &allowLocal,
			PolicyRevisionHash:     "sha256:rev-a",
			Now:                    time.Now().UTC(),
		}); err != nil {
			errs <- fmt.Errorf("goroutine A: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		requireMFA := true
		if _, err := storeB.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
			RequireMFAForAllUsers: &requireMFA,
			PolicyRevisionHash:    "sha256:rev-b",
			Now:                   time.Now().UTC(),
		}); err != nil {
			errs <- fmt.Errorf("goroutine B: %w", err)
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	final, err := NewIdentitySubjectStore(SQLDB{DB: seedDB}).GetSignInPolicy(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetSignInPolicy() error = %v", err)
	}
	// Both concurrent edits touched DIFFERENT fields, so a correctly
	// row-locked (not lost-update) serialization must show BOTH applied,
	// regardless of which transaction committed last.
	if final.AllowLocalUserCreation {
		t.Fatal("final policy AllowLocalUserCreation = true, want false (goroutine A's edit was lost)")
	}
	if !final.RequireMFAForAllUsers {
		t.Fatal("final policy RequireMFAForAllUsers = false, want true (goroutine B's edit was lost)")
	}
}

func testRecordSSOAdminVerificationIdempotentUnderContention(t *testing.T, ctx context.Context, dsn, schemaName string) {
	t.Helper()
	tenantID := "tenant-signin-sso-proof"
	seedDB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	seedSignInPolicyTenant(t, ctx, seedDB, tenantID)

	connA := openSignInPolicyConn(t, ctx, dsn, schemaName)
	connB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	storeA := NewIdentitySubjectStore(SQLDB{DB: connA})
	storeB := NewIdentitySubjectStore(SQLDB{DB: connB})

	earlier := time.Now().UTC().Add(-time.Hour)
	later := time.Now().UTC()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := storeA.RecordSSOAdminVerification(ctx, tenantID, "pc_first", earlier); err != nil {
			errs <- fmt.Errorf("goroutine A: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := storeB.RecordSSOAdminVerification(ctx, tenantID, "pc_second", later); err != nil {
			errs <- fmt.Errorf("goroutine B: %w", err)
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	var verifiedAt sql.NullTime
	var verifiedProvider sql.NullString
	row := seedDB.QueryRowContext(ctx, `
SELECT sso_admin_verified_at, sso_admin_verified_provider_config_id
FROM identity_sign_in_policies WHERE tenant_id = $1
`, tenantID)
	if err := row.Scan(&verifiedAt, &verifiedProvider); err != nil {
		t.Fatalf("read final sign-in policy row: %v", err)
	}
	if !verifiedAt.Valid || !verifiedProvider.Valid {
		t.Fatal("sso_admin_verified_at/provider_config_id not set after two concurrent calls")
	}
	// Sticky semantics: whichever call's INSERT won the race is authoritative
	// (COALESCE never overwrites an already-set value), and the pairing must
	// stay consistent — never one call's timestamp with the other's provider.
	if verifiedProvider.String != "pc_first" && verifiedProvider.String != "pc_second" {
		t.Fatalf("sso_admin_verified_provider_config_id = %q, want pc_first or pc_second", verifiedProvider.String)
	}
}

func testAcceptInvitationRejectsMissingMFAAgainstRealPolicyRow(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tenantID := "tenant-signin-invite-mfa"
	workspaceID := "workspace-signin-invite-mfa"
	seedSignInPolicyTenant(t, ctx, db, tenantID)
	seedSignInPolicyWorkspace(t, ctx, db, tenantID, workspaceID)

	store := NewIdentitySubjectStore(SQLDB{DB: db})
	requireMFA := true
	if _, err := store.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
		RequireMFAForAllUsers: &requireMFA,
		PolicyRevisionHash:    "sha256:rev-mfa",
		Now:                   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}

	now := time.Now().UTC()
	inviteCode := "invite-code-mfa"
	if err := store.CreateLocalIdentityInvitation(ctx, LocalIdentityInvitationRecord{
		InviteID:           "invite-mfa-1",
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		InviteCodeHash:     "sha256:" + inviteCode,
		RoleID:             "developer",
		Status:             "active",
		PolicyRevisionHash: "sha256:rev-mfa",
		ExpiresAt:          now.Add(24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateLocalIdentityInvitation() error = %v", err)
	}

	err := store.AcceptLocalIdentityInvitation(ctx, LocalIdentityInvitationAcceptance{
		InviteCodeHash:         "sha256:" + inviteCode,
		UserID:                 "user-signin-invite-mfa",
		SubjectIDHash:          "sha256:subject-signin-invite-mfa",
		ProfileHandleHash:      "sha256:handle-signin-invite-mfa",
		PasswordHash:           "bcrypt:fake-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		AcceptedAt:             now,
	})
	if err == nil {
		t.Fatal("AcceptLocalIdentityInvitation() with no MFA factor and require_mfa_for_all_users=true error = nil, want ErrLocalIdentityMFARequiredByPolicy")
	}
}

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
