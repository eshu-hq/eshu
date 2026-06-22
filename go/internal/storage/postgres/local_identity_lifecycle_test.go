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
