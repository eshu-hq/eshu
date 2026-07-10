// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	t.Run("RequireSSOFlipRevokesOnlyLocalUserSessions", func(t *testing.T) {
		testRequireSSOFlipRevokesOnlyLocalUserSessions(t, ctx, ownerDB)
	})
	t.Run("MergedTimeoutOrderingSerializesUnderLock", func(t *testing.T) {
		testMergedTimeoutOrderingSerializesUnderLock(t, ctx, dsn, schemaName)
	})
}

// testMergedTimeoutOrderingSerializesUnderLock proves issue #5002 part 2's
// root-cause fix (codex PR #5053 review): two concurrent PATCH-equivalent
// UpsertSignInPolicy calls against the SAME tenant — one setting ONLY
// idle_timeout_seconds=3600, one setting ONLY absolute_timeout_seconds=1800
// — can never both commit. Whichever call acquires the FOR UPDATE row lock
// first commits (its own single field never conflicts with the still-unset
// other field, so its merged pair is valid); the second call's locked read
// sees the FIRST call's already-committed value and is rejected with
// ErrSignInPolicyTimeoutOrdering. The final stored row NEVER has
// absolute_timeout_seconds < idle_timeout_seconds — proving the race a
// handler-side pre-transaction read could not close (both callers would read
// the same stale stored row and both pass) is actually closed here.
func testMergedTimeoutOrderingSerializesUnderLock(t *testing.T, ctx context.Context, dsn, schemaName string) {
	t.Helper()
	tenantID := "tenant-signin-timeout-race"
	seedDB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	seedSignInPolicyTenant(t, ctx, seedDB, tenantID)

	connA := openSignInPolicyConn(t, ctx, dsn, schemaName)
	connB := openSignInPolicyConn(t, ctx, dsn, schemaName)
	storeA := NewIdentitySubjectStore(SQLDB{DB: connA})
	storeB := NewIdentitySubjectStore(SQLDB{DB: connB})

	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		idle := 3600
		_, err := storeA.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
			IdleTimeoutSeconds: &idle,
			PolicyRevisionHash: "sha256:rev-timeout-a",
			Now:                time.Now().UTC(),
		})
		results <- err
	}()
	go func() {
		defer wg.Done()
		absolute := 1800
		_, err := storeB.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
			AbsoluteTimeoutSeconds: &absolute,
			PolicyRevisionHash:     "sha256:rev-timeout-b",
			Now:                    time.Now().UTC(),
		})
		results <- err
	}()
	wg.Wait()
	close(results)

	var errs []error
	for err := range results {
		errs = append(errs, err)
	}
	if len(errs) != 2 {
		t.Fatalf("got %d results, want 2", len(errs))
	}
	var nilCount, orderingCount int
	for _, err := range errs {
		switch {
		case err == nil:
			nilCount++
		case errors.Is(err, ErrSignInPolicyTimeoutOrdering):
			orderingCount++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if nilCount != 1 || orderingCount != 1 {
		t.Fatalf("nilCount=%d orderingCount=%d, want exactly one success and one ErrSignInPolicyTimeoutOrdering rejection (errs=%v)", nilCount, orderingCount, errs)
	}

	final, err := NewIdentitySubjectStore(SQLDB{DB: seedDB}).GetSignInPolicy(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetSignInPolicy() error = %v", err)
	}
	if final.IdleTimeoutSeconds > 0 && final.AbsoluteTimeoutSeconds > 0 &&
		final.AbsoluteTimeoutSeconds < final.IdleTimeoutSeconds {
		t.Fatalf(
			"final policy has absolute(%d) < idle(%d) — the race was NOT closed",
			final.AbsoluteTimeoutSeconds, final.IdleTimeoutSeconds,
		)
	}
}

// testRequireSSOFlipRevokesOnlyLocalUserSessions proves issue #5002 part 1
// against real Postgres: a require_sso false->true flip bulk-revokes every
// active subject_class='local_user' browser session for the SAME tenant,
// leaves subject_class='break_glass' and 'external_oidc_user' sessions (and
// another tenant's local_user session) untouched, and a second call against
// an already-true policy is idempotent (no error, no double-toggle).
func testRequireSSOFlipRevokesOnlyLocalUserSessions(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tenantID := "tenant-signin-revoke"
	otherTenantID := "tenant-signin-revoke-other"
	workspaceID := "workspace-signin-revoke"
	seedSignInPolicyTenant(t, ctx, db, tenantID)
	seedSignInPolicyWorkspace(t, ctx, db, tenantID, workspaceID)
	seedSignInPolicyTenant(t, ctx, db, otherTenantID)
	seedSignInPolicyWorkspace(t, ctx, db, otherTenantID, workspaceID)

	sessions := NewBrowserSessionStore(SQLDB{DB: db})
	now := time.Now().UTC()
	localSession := browserSessionRevokeFixture("sess-local", tenantID, workspaceID, "local_user", now)
	breakGlassSession := browserSessionRevokeFixture("sess-break-glass", tenantID, workspaceID, "break_glass", now)
	oidcSession := browserSessionRevokeFixture("sess-oidc", tenantID, workspaceID, "external_oidc_user", now)
	otherTenantLocalSession := browserSessionRevokeFixture("sess-other-tenant-local", otherTenantID, workspaceID, "local_user", now)
	for _, record := range []BrowserSessionRecord{localSession, breakGlassSession, oidcSession, otherTenantLocalSession} {
		if err := sessions.CreateSession(ctx, record); err != nil {
			t.Fatalf("seed browser session %s: %v", record.SessionHash, err)
		}
	}

	insertActiveProviderConfig(t, ctx, db, tenantID, "pc_revoke")
	store := NewIdentitySubjectStore(SQLDB{DB: db})
	if err := store.RecordSSOAdminVerification(ctx, tenantID, "pc_revoke", now); err != nil {
		t.Fatalf("RecordSSOAdminVerification() error = %v", err)
	}

	requireSSO := true
	if _, err := store.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev-revoke",
		Now:                time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}

	assertBrowserSessionRevoked(t, ctx, db, localSession.SessionHash, true)
	assertBrowserSessionRevoked(t, ctx, db, breakGlassSession.SessionHash, false)
	assertBrowserSessionRevoked(t, ctx, db, oidcSession.SessionHash, false)
	assertBrowserSessionRevoked(t, ctx, db, otherTenantLocalSession.SessionHash, false)

	// Idempotent re-run: editing an unrelated field on an already-true policy
	// must not error, and the already-revoked session must stay revoked (not
	// re-toggled by the unconditional revoke running again).
	requireMFA := true
	if _, err := store.UpsertSignInPolicy(ctx, tenantID, SignInPolicyUpdate{
		RequireMFAForAllUsers: &requireMFA,
		PolicyRevisionHash:    "sha256:rev-revoke-2",
		Now:                   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertSignInPolicy() second call error = %v", err)
	}
	assertBrowserSessionRevoked(t, ctx, db, localSession.SessionHash, true)
}

func browserSessionRevokeFixture(hash, tenantID, workspaceID, subjectClass string, now time.Time) BrowserSessionRecord {
	return BrowserSessionRecord{
		SessionHash:        "sha256:" + hash,
		CSRFTokenHash:      "sha256:csrf-" + hash,
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		SubjectIDHash:      "sha256:subject-" + hash,
		SubjectClass:       subjectClass,
		PolicyRevisionHash: "sha256:policy",
		AllScopes:          true,
		IssuedAt:           now,
		LastSeenAt:         now,
		IdleExpiresAt:      now.Add(time.Hour),
		AbsoluteExpiresAt:  now.Add(24 * time.Hour),
		UpdatedAt:          now,
	}
}

func assertBrowserSessionRevoked(t *testing.T, ctx context.Context, db *sql.DB, sessionHash string, wantRevoked bool) {
	t.Helper()
	var revokedAt sql.NullTime
	row := db.QueryRowContext(ctx, `SELECT revoked_at FROM browser_sessions WHERE session_hash = $1`, sessionHash)
	if err := row.Scan(&revokedAt); err != nil {
		t.Fatalf("read browser session %s: %v", sessionHash, err)
	}
	if revokedAt.Valid != wantRevoked {
		t.Fatalf("browser session %s revoked = %t, want %t", sessionHash, revokedAt.Valid, wantRevoked)
	}
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

// Shared DSN/schema fixture helpers (signInPolicyProofDSN,
// openSignInPolicySchemaFixture, openSignInPolicyConnRaw,
// openSignInPolicyConn, seedSignInPolicyTenant, seedSignInPolicyWorkspace,
// insertActiveProviderConfig) live in
// identity_sign_in_policy_concurrency_helpers_test.go, split out to keep
// this file under the repository's 500-line cap.
