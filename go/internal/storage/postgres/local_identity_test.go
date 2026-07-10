// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticateLocalIdentityRequiresMFAForOwner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 15, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, "correct-password"),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		true,
		true,
		"sha256:policy",
	})
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:owner-subject",
		Password:      "correct-password",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthMFARequired || result.Authenticated {
		t.Fatalf("auth result = %#v, want MFA required without authenticated session", result)
	}
}

func TestAuthenticateLocalIdentityConsumesRecoveryCode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 20, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, "correct-password"),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		true,
		true,
		"sha256:policy",
	})
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:         "sha256:owner-subject",
		Password:              "correct-password",
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
	if !result.Auth.AllScopes || result.Auth.SubjectIDHash != "sha256:owner-subject" {
		t.Fatalf("auth context = %#v, want owner all-scopes subject", result.Auth)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("auth execs missing recovery code consumption: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "DELETE FROM identity_local_auth_attempts") {
		t.Fatalf("auth execs missing lockout reset: %#v", db.execs)
	}
	for _, exec := range db.execs {
		if fakeExecArgsContain(exec.args, "correct-password") {
			t.Fatalf("auth args leaked raw password: %#v", exec.args)
		}
	}
}

func TestAuthenticateLocalIdentityPreservesPasswordWhitespace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 22, 0, 0, time.UTC)
	password := "  correct-password  "
	db := localIdentityAuthDB(t, password, []any{
		"user_member",
		"tenant_local",
		"workspace_local",
		"sha256:member-subject",
		mustBcryptHash(t, password),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		false,
		false,
		"sha256:policy",
	})
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      password,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
}

func TestAuthenticateLocalIdentityNonAdminResolvesPermissionGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 13, 0, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"user_member",
			"tenant_local",
			"workspace_local",
			"sha256:member-subject",
			mustBcryptHash(t, password),
			"active",
			sql.NullTime{},
			sql.NullTime{},
			int64(0),
			false, // has_admin_role
			false, // has_active_mfa
			"sha256:policy",
		}}},
		{rows: nil}, // signInPolicyRequiresMFAForUsers: off (regression guard, issue #5001)
		{rows: [][]any{{"role_reader"}}},
		{rows: [][]any{
			{"ask_search", "ask_reasoning"},
			{"repository_content", "source_content"},
		}},
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
	if result.Status != LocalIdentityAuthAuthenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
	if result.Auth.AllScopes {
		t.Fatalf("non-admin auth AllScopes = true, want false")
	}
	if !result.Auth.PermissionCatalogEnforced {
		t.Fatalf("non-admin auth PermissionCatalogEnforced = false, want true")
	}
	if got, want := result.Auth.RoleIDs, []string{"role_reader"}; !equalStringSlices(got, want) {
		t.Fatalf("RoleIDs = %#v, want %#v", got, want)
	}
	if got, want := result.Auth.AllowedPermissionFeatures, []string{"ask_search", "repository_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionFeatures = %#v, want %#v", got, want)
	}
	if got, want := result.Auth.AllowedPermissionDataClasses, []string{"ask_reasoning", "source_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionDataClasses = %#v, want %#v", got, want)
	}
	if !fakeQueriesContain(db.queries, "FROM identity_membership_roles role_assignment") {
		t.Fatalf("auth queries missing local role resolution: %#v", db.queries)
	}
	if !fakeQueriesContain(db.queries, "FROM identity_role_grants role_grant") {
		t.Fatalf("auth queries missing permission-grant resolution: %#v", db.queries)
	}
}

func TestAuthenticateLocalIdentityAdminStaysFailOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 13, 30, 0, 0, time.UTC)
	password := "correct-password"
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
		// No second entry: admins never read require_mfa_for_all_users (issue
		// #5001 P1 review finding — admin login must survive a policy-read
		// outage for break-glass).
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:owner-subject",
		Password:            password,
		MFARecoveryCodeHash: "sha256:recovery-a",
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if !result.Auth.AllScopes {
		t.Fatalf("admin auth AllScopes = false, want true")
	}
	if result.Auth.PermissionCatalogEnforced {
		t.Fatalf("admin auth PermissionCatalogEnforced = true, want false (must stay fail-open)")
	}
	if len(result.Auth.AllowedPermissionFeatures) != 0 || len(result.Auth.AllowedPermissionDataClasses) != 0 {
		t.Fatalf("admin auth carries permission grants = %#v/%#v, want empty", result.Auth.AllowedPermissionFeatures, result.Auth.AllowedPermissionDataClasses)
	}
	if fakeQueriesContain(db.queries, "FROM identity_membership_roles role_assignment") {
		t.Fatalf("admin auth must not resolve roles for enforcement: %#v", db.queries)
	}
}

func TestAuthenticateLocalIdentityLocksAfterFailedPassword(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 25, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, "correct-password"),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(defaultLocalIdentityLockoutThreshold - 1),
		true,
		true,
		"sha256:policy",
	})
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:owner-subject",
		Password:      "wrong-password",
		Now:           now,
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
	if !strings.Contains(db.execs[0].query, "identity_local_auth_attempts.failed_attempts + 1") {
		t.Fatalf("failed-attempt upsert is not atomic:\n%s", db.execs[0].query)
	}
}

func TestAuthenticateLocalIdentityLocksAfterFailedMFARecoveryCode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 27, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, "correct-password"),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(defaultLocalIdentityLockoutThreshold - 1),
		true,
		true,
		"sha256:policy",
	})
	db.execResults = []sql.Result{fakeRowsAffected{n: 0}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:owner-subject",
		Password:            "correct-password",
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
	if got := len(db.execs); got != 2 {
		t.Fatalf("auth exec count = %d, want recovery consume plus failed-attempt upsert", got)
	}
}

func TestAcceptLocalIdentityInvitationRequiresActiveInvite(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{}},
		},
	}
	store := NewIdentitySubjectStore(db)

	err := store.AcceptLocalIdentityInvitation(context.Background(), LocalIdentityInvitationAcceptance{
		InviteCodeHash:         "sha256:missing-invite",
		UserID:                 "user_member",
		SubjectIDHash:          "sha256:member-subject",
		PasswordHash:           "bcrypt:member-password-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		AcceptedAt:             time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrLocalIdentityInvitationRequired) {
		t.Fatalf("AcceptLocalIdentityInvitation() error = %v, want ErrLocalIdentityInvitationRequired", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
	if !strings.Contains(db.queries[0].query, "FOR UPDATE") {
		t.Fatalf("invitation acceptance query missing row lock:\n%s", db.queries[0].query)
	}
}

func TestResolveLocalIdentityBreakGlassDisabledByDefault(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewIdentitySubjectStore(db)

	_, err := store.ResolveLocalIdentityBreakGlass(context.Background(), LocalIdentityBreakGlassAttempt{
		BreakGlassCodeHash: "sha256:break-glass",
		Now:                time.Date(2026, 6, 22, 12, 35, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrLocalIdentityBreakGlassUnavailable) {
		t.Fatalf("ResolveLocalIdentityBreakGlass() error = %v, want ErrLocalIdentityBreakGlassUnavailable", err)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"UPDATE identity_break_glass_windows",
		"used_at = $2",
		"status = 'active'",
		"expires_at > $2",
		"RETURNING tenant_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("break-glass query missing %q:\n%s", want, query)
		}
	}
}

func completeBootstrapRecord() LocalIdentityBootstrapRecord {
	now := time.Date(2026, 6, 22, 12, 10, 0, 0, time.UTC)
	return LocalIdentityBootstrapRecord{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		UserID:                 "user_owner",
		SubjectIDHash:          "sha256:owner-subject",
		PasswordHash:           "bcrypt:owner-password-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		MFAFactorID:            "mfa_owner",
		MFAFactorKind:          "recovery_code",
		MFACredentialHandle:    "handle:owner-mfa",
		RecoveryCodeHashes:     []string{"sha256:recovery-a"},
		PolicyRevisionHash:     "sha256:policy",
		CreatedAt:              now,
	}
}

func localIdentityAuthDB(t *testing.T, password string, row []any) *fakeExecQueryer {
	t.Helper()
	if row[4] == "" {
		row[4] = mustBcryptHash(t, password)
	}
	// Non-admin logins resolve role IDs and then permission grants after the
	// credential select. Admin logins short-circuit before those queries, so the
	// trailing empty responses are simply unused for the admin case.
	return &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{row}},
		{rows: [][]any{}},
		{rows: [][]any{}},
	}}
}

func mustBcryptHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	return string(hash)
}

func fakeQueriesContain(queries []fakeQueryCall, want string) bool {
	for _, q := range queries {
		if strings.Contains(q.query, want) {
			return true
		}
	}
	return false
}

func fakeExecsContainQuery(execs []fakeExecCall, want string) bool {
	for _, exec := range execs {
		if strings.Contains(exec.query, want) {
			return true
		}
	}
	return false
}
