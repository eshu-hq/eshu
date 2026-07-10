// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// nonAdminAuthCredentialRow returns the selectLocalIdentityCredentialQuery row
// shape for a non-admin local user, used by the require_mfa_for_all_users
// login-time enforcement tests below (issue #5001). Before this change,
// AuthenticateLocalIdentity gated MFA on HasAdminRole only, so a non-admin
// local user was never re-checked for MFA at login even when the tenant's
// require_mfa_for_all_users sign-in policy was on.
func nonAdminAuthCredentialRow(t *testing.T, password string, hasActiveMFA bool, failedAttempts int64) []any {
	t.Helper()
	return []any{
		"user_member",
		"tenant_local",
		"workspace_local",
		"sha256:member-subject",
		mustBcryptHash(t, password),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		failedAttempts,
		false, // has_admin_role
		hasActiveMFA,
		"sha256:policy",
	}
}

func TestAuthenticateLocalIdentityNonAdminMFARequiredWhenPolicyOnNoFactor(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, false, 0)}}, // selectLocalIdentityCredential
		{rows: [][]any{{true}}}, // signInPolicyRequiresMFAForUsers: on
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthMFARequired || result.Authenticated {
		t.Fatalf("auth result = %#v, want MFA required without a session", result)
	}
}

func TestAuthenticateLocalIdentityNonAdminMFARequiredWhenPolicyOnFactorNoRecoveryCode(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, true, 0)}}, // selectLocalIdentityCredential, has a factor
		{rows: [][]any{{true}}}, // signInPolicyRequiresMFAForUsers: on
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           time.Date(2026, 7, 10, 9, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthMFARequired || result.Authenticated {
		t.Fatalf("auth result = %#v, want MFA required without a session", result)
	}
}

func TestAuthenticateLocalIdentityNonAdminAuthenticatesWithValidRecoveryCodeWhenPolicyOn(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	now := time.Date(2026, 7, 10, 9, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, true, 0)}}, // selectLocalIdentityCredential
		{rows: [][]any{{true}}},          // signInPolicyRequiresMFAForUsers: on
		{rows: [][]any{{"role_reader"}}}, // resolveLocalIdentityRoles
		{rows: [][]any{
			{"ask_search", "ask_reasoning"},
			{"repository_content", "source_content"},
		}}, // resolvePermissionGrantsForRoles
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:         "sha256:member-subject",
		Password:              password,
		MFARecoveryCodeHash:   "sha256:recovery-a",
		ConsumeRecoveryCodeAt: now,
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
	if result.Auth.AllScopes {
		t.Fatalf("non-admin auth AllScopes = true, want false")
	}
	if !result.Auth.PermissionCatalogEnforced {
		t.Fatalf("non-admin auth PermissionCatalogEnforced = false, want true (permission-catalog snapshot still attached)")
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("auth execs missing recovery code consumption: %#v", db.execs)
	}
}

func TestAuthenticateLocalIdentityNonAdminLocksAfterInvalidRecoveryCodeWhenPolicyOn(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	now := time.Date(2026, 7, 10, 9, 15, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, true, defaultLocalIdentityLockoutThreshold-1)}},
		{rows: [][]any{{true}}}, // signInPolicyRequiresMFAForUsers: on
	}}
	db.execResults = []sql.Result{fakeRowsAffected{n: 0}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:member-subject",
		Password:            password,
		MFARecoveryCodeHash: "sha256:wrong-recovery",
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthLocked || result.Authenticated {
		t.Fatalf("auth result = %#v, want locked", result)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_auth_attempts") {
		t.Fatalf("auth execs missing failed-attempt upsert: %#v", db.execs)
	}
}

// TestAuthenticateLocalIdentityNonAdminPolicyOffAuthenticatesWithPasswordOnly
// is the regression guard: a non-admin local user on a tenant with
// require_mfa_for_all_users off must keep authenticating with password only,
// exactly as before this change.
func TestAuthenticateLocalIdentityNonAdminPolicyOffAuthenticatesWithPasswordOnly(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	now := time.Date(2026, 7, 10, 9, 20, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, false, 0)}}, // selectLocalIdentityCredential
		{rows: nil},                      // signInPolicyRequiresMFAForUsers: off (no row)
		{rows: [][]any{{"role_reader"}}}, // resolveLocalIdentityRoles
		{rows: [][]any{{"ask_search", "ask_reasoning"}}}, // resolvePermissionGrantsForRoles
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated with password only", result)
	}
	if fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("auth execs unexpectedly consumed a recovery code: %#v", db.execs)
	}
}

// TestAuthenticateLocalIdentityMFAAllUsersPolicyReadErrorDeniesLogin proves
// the login-time require_mfa_for_all_users read fails CLOSED for a non-admin:
// a read error denies the login (no session issued) rather than silently
// skipping the check, mirroring the require_sso non-admin fail-closed stance
// in go/internal/query/local_identity_sign_in_policy_gate.go.
func TestAuthenticateLocalIdentityMFAAllUsersPolicyReadErrorDeniesLogin(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, false, 0)}},
		{err: errors.New("policy read boom")},
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           time.Date(2026, 7, 10, 9, 25, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("AuthenticateLocalIdentity() error = nil, want policy read error")
	}
	if result.Authenticated || result.Status != "" {
		t.Fatalf("auth result = %#v, want zero value on policy read failure", result)
	}
}

// TestAuthenticateLocalIdentityAdminAuthenticatesWithoutPolicyReadEvenIfItWouldError
// is the P1 review-finding regression guard (PR #5049 Codex review): admins
// always require MFA regardless of require_mfa_for_all_users, so the login
// path must NEVER read that policy for an admin. Before this fix the read was
// unconditional, so an identity_sign_in_policies outage denied a local ADMIN
// login before the handler's documented policy_read_error_admin_allowed
// break-glass path (local_identity_sign_in_policy_gate.go) ever got a chance
// to apply. This test stages NO second queryResponses entry at all: if the
// admin path issues a second QueryContext call for any reason, the fake
// returns "unexpected query" and the login fails, proving the absence of the
// read rather than merely a lucky ordering.
func TestAuthenticateLocalIdentityAdminAuthenticatesWithoutPolicyReadEvenIfItWouldError(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	now := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"user_owner",
			"tenant_local",
			"workspace_local",
			"sha256:owner-subject",
			mustBcryptHash(t, password),
			"active",
			sql.NullTime{},
			sql.NullTime{},
			int64(0),
			true, // has_admin_role
			true, // has_active_mfa
			"sha256:policy",
		}}},
		// Deliberately no second entry: an admin login must not issue a
		// signInPolicyRequiresMFAForUsers query at all.
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:owner-subject",
		Password:            password,
		MFARecoveryCodeHash: "sha256:recovery-a",
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v, want admin login to succeed without any policy read", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("admin login issued %d queries, want exactly 1 (credential select only, no policy read): %#v", got, db.queries)
	}
}

// TestAuthenticateLocalIdentityNonAdminMFARequiredCarriesAuthContext is the
// P2 review-finding regression guard (PR #5049 Codex review): the non-admin
// mfa_required result must carry enough LocalIdentityAuthContext for the
// handler to run requireSSODecision BEFORE issuing the mfa_required response,
// so require_sso=true correctly takes precedence over an mfa_required
// challenge a non-admin could never complete via local login anyway. The
// result itself stays unauthenticated (no session) — Auth here is read-only
// input to the handler's require_sso gate, not proof of a session.
func TestAuthenticateLocalIdentityNonAdminMFARequiredCarriesAuthContext(t *testing.T) {
	t.Parallel()

	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{nonAdminAuthCredentialRow(t, password, false, 0)}}, // selectLocalIdentityCredential
		{rows: [][]any{{true}}}, // signInPolicyRequiresMFAForUsers: on
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           time.Date(2026, 7, 10, 10, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthMFARequired || result.Authenticated {
		t.Fatalf("auth result = %#v, want MFA required without a session", result)
	}
	if result.Auth.TenantID != "tenant_local" || result.Auth.SubjectIDHash != "sha256:member-subject" {
		t.Fatalf("mfa_required Auth = %#v, want tenant/subject populated for the handler's require_sso gate", result.Auth)
	}
	if result.Auth.AllScopes {
		t.Fatalf("mfa_required Auth.AllScopes = true, want false for a non-admin")
	}
}

// TestAuthenticateLocalIdentityMFAEnforcementIsASingleSharedCodePath is the
// hermetic, credential-free regression guard mirroring the static-source-text
// pattern in TestResolvePermissionGrantsQueryDoesNotAliasReservedWord above:
// it reads the shipped identity_local.go source and asserts the admin and
// require_mfa_for_all_users non-admin MFA checks share ONE enforcement block
// (issue #5001) rather than each branch forking its own copy of the recovery
// -code consumption and mfa_required return. A future edit that duplicates
// the block instead of reusing it — an easy mistake when adding a new
// caller-specific MFA path — flips this guard red.
func TestAuthenticateLocalIdentityMFAEnforcementIsASingleSharedCodePath(t *testing.T) {
	source, err := os.ReadFile("identity_local.go")
	if err != nil {
		t.Fatalf("read identity_local.go: %v", err)
	}
	text := string(source)

	if got := strings.Count(text, "consumeLocalIdentityRecoveryCode(ctx, s.db, row.UserID, attempt)"); got != 1 {
		t.Fatalf("consumeLocalIdentityRecoveryCode call count in AuthenticateLocalIdentity = %d, want 1 (admin and require_mfa_for_all_users non-admin must share one enforcement block)", got)
	}
	if got := strings.Count(text, "Status: LocalIdentityAuthMFARequired,"); got != 1 {
		t.Fatalf("mfa_required return count = %d, want 1 (admin and non-admin must share one enforcement block)", got)
	}
	if !strings.Contains(text, "signInPolicyRequiresMFAForUsers(ctx, s.db, row.TenantID)") {
		t.Fatalf("AuthenticateLocalIdentity must read require_mfa_for_all_users via signInPolicyRequiresMFAForUsers for the authenticated row's own tenant")
	}
}
