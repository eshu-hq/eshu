package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCreateLocalIdentityInvitationWritesHashOnlyAssignment(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.CreateLocalIdentityInvitation(context.Background(), LocalIdentityInvitationRecord{
		InviteID:           "invite_member",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		InviteCodeHash:     "sha256:invite-code",
		InviteeHandleHash:  "sha256:invitee-handle",
		RoleID:             "developer",
		Status:             "active",
		PolicyRevisionHash: "sha256:policy",
		ExpiresAt:          now.Add(24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		t.Fatalf("CreateLocalIdentityInvitation() error = %v", err)
	}
	if len(db.execs) != 1 || !strings.Contains(db.execs[0].query, "INSERT INTO identity_invitations") {
		t.Fatalf("invitation execs = %#v, want identity_invitations insert", db.execs)
	}
	if fakeExecArgsContain(db.execs[0].args, "invite-secret") || fakeExecArgsContain(db.execs[0].args, "member@example.test") {
		t.Fatalf("invitation args leaked raw invite material: %#v", db.execs[0].args)
	}
}

func TestResetLocalIdentityPasswordRevokesOldCredentialAndClearsLockout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 5, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.ResetLocalIdentityPassword(context.Background(), LocalIdentityPasswordReset{
		UserID:                 "user_owner",
		CredentialID:           "credential_owner_rotated",
		PasswordHash:           "bcrypt:rotated-password-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		ResetAt:                now,
	})
	if err != nil {
		t.Fatalf("ResetLocalIdentityPassword() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	for _, want := range []string{
		"UPDATE identity_local_credentials",
		"INSERT INTO identity_local_credentials",
		"DELETE FROM identity_local_auth_attempts",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("password reset execs missing %q: %#v", want, db.execs)
		}
	}
}

func TestResetLocalIdentityMFARevokesFactorsAndRecoveryCodes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 10, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.ResetLocalIdentityMFA(context.Background(), LocalIdentityMFAReset{
		UserID:              "user_owner",
		MFAFactorID:         "mfa_owner_rotated",
		MFAFactorKind:       "recovery_code",
		MFACredentialHandle: "handle:owner-mfa-rotated",
		RecoveryCodeHashes:  []string{"sha256:rotated-recovery"},
		ResetAt:             now,
	})
	if err != nil {
		t.Fatalf("ResetLocalIdentityMFA() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	for _, want := range []string{
		"UPDATE identity_mfa_recovery_codes",
		"UPDATE identity_mfa_factors",
		"INSERT INTO identity_mfa_factors",
		"INSERT INTO identity_mfa_recovery_codes",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("MFA reset execs missing %q: %#v", want, db.execs)
		}
	}
}

func TestDisableLocalIdentityUserRevokesCredentialsAndBrowserSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 15, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.DisableLocalIdentityUser(context.Background(), LocalIdentityDisableUser{
		UserID:     "user_owner",
		DisabledAt: now,
	})
	if err != nil {
		t.Fatalf("DisableLocalIdentityUser() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	for _, want := range []string{
		"UPDATE identity_users",
		"UPDATE identity_local_credentials",
		"UPDATE identity_mfa_factors",
		"UPDATE browser_sessions",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("disable execs missing %q: %#v", want, db.execs)
		}
	}
}

func TestEnableLocalIdentityBreakGlassRequiresTimeBoxAndAuditSafeCodeHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 20, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.EnableLocalIdentityBreakGlass(context.Background(), LocalIdentityBreakGlassWindow{
		RecoveryID:         "recovery_window",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		SubjectIDHash:      "sha256:owner-subject",
		BreakGlassCodeHash: "sha256:break-glass-code",
		Status:             "active",
		ReasonCode:         "operator_recovery",
		PolicyRevisionHash: "sha256:policy",
		EnabledAt:          now,
		ExpiresAt:          now.Add(15 * time.Minute),
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		t.Fatalf("EnableLocalIdentityBreakGlass() error = %v", err)
	}
	if len(db.execs) != 1 || !strings.Contains(db.execs[0].query, "INSERT INTO identity_break_glass_windows") {
		t.Fatalf("break-glass execs = %#v, want identity_break_glass_windows insert", db.execs)
	}
	if fakeExecArgsContain(db.execs[0].args, "break-glass-secret") {
		t.Fatalf("break-glass args leaked raw code: %#v", db.execs[0].args)
	}
}

func TestCreateLocalIdentityAPITokenStoresHashOnlyPersonalToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.CreateLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenCreate{
		TokenID:            "token_personal",
		TokenHash:          "sha256:generated-token",
		TokenClass:         "personal",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		UserID:             "user_owner",
		DisplayHandleHash:  "sha256:display",
		PolicyRevisionHash: "sha256:policy",
		IssuedAt:           now,
		ExpiresAt:          now.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateLocalIdentityAPIToken() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO identity_token_metadata",
		"FROM identity_users user_subject",
		"JOIN identity_tenant_memberships membership",
		"user_subject.status = 'active'",
		"membership.status = 'active'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("personal token query missing %q:\n%s", want, query)
		}
	}
	if fakeExecArgsContain(db.execs[0].args, "raw-generated-token") ||
		fakeExecArgsContain(db.execs[0].args, "owner laptop") {
		t.Fatalf("token create args leaked raw material: %#v", db.execs[0].args)
	}
}

func TestCreateLocalIdentityAPITokenStoresHashOnlyServicePrincipalToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)

	err := store.CreateLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenCreate{
		TokenID:            "token_service",
		TokenHash:          "sha256:generated-token",
		TokenClass:         "service_principal",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		ServicePrincipalID: "svc_worker",
		PolicyRevisionHash: "sha256:policy",
		IssuedAt:           now,
	})
	if err != nil {
		t.Fatalf("CreateLocalIdentityAPIToken() error = %v", err)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO identity_token_metadata",
		"FROM identity_service_principals service_principal",
		"service_principal.owner_user_id IS NOT NULL",
		"service_principal.status = 'active'",
		"service_principal.disabled_at IS NULL",
		"service_principal.tombstoned_at IS NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("service principal token query missing %q:\n%s", want, query)
		}
	}
	if fakeExecArgsContain(db.execs[0].args, "raw-generated-token") {
		t.Fatalf("service token args leaked raw material: %#v", db.execs[0].args)
	}
}

func TestRevokeAndRotateLocalIdentityAPITokenUpdateActiveMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 10, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	store := NewIdentitySubjectStore(db)

	if err := store.RevokeLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRevoke{
		TokenID:     "token_old",
		TenantID:    "tenant_local",
		WorkspaceID: "workspace_local",
		RevokedAt:   now,
	}); err != nil {
		t.Fatalf("RevokeLocalIdentityAPIToken() error = %v", err)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_token_metadata") ||
		!fakeExecsContainQuery(db.execs, "revoked_at = $4") {
		t.Fatalf("revoke execs missing active metadata update: %#v", db.execs)
	}

	if err := store.RotateLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRotate{
		OldTokenID:      "token_old",
		NewTokenID:      "token_new",
		NewTokenHash:    "sha256:new-generated-token",
		TenantID:        "tenant_local",
		WorkspaceID:     "workspace_local",
		RotatedAt:       now.Add(time.Minute),
		NewTokenExpires: now.Add(7 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("RotateLocalIdentityAPIToken() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	for _, want := range []string{
		"UPDATE identity_token_metadata",
		"INSERT INTO identity_token_metadata",
		"FROM identity_token_metadata old_token",
		"old_token.status = 'active'",
		"old_token.revoked_at IS NULL",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("rotate execs missing %q: %#v", want, db.execs)
		}
	}
	if fakeExecArgsContain(db.execs[len(db.execs)-1].args, "raw-new-token") {
		t.Fatalf("rotate args leaked raw token: %#v", db.execs[len(db.execs)-1].args)
	}
}
