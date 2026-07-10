// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// TestBootstrapLocalIdentityEnvSeededSetsMustChangePassword proves the
// env-seeded bootstrap path (go/cmd/api/seed_initial_admin.go
// seedBootstrapAdminFromEnv) threads MustChangePassword=true all the way
// through BootstrapLocalIdentity into the credential INSERT (issue #4976).
func TestBootstrapLocalIdentityEnvSeededSetsMustChangePassword(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(0)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	record := completeBootstrapRecord()
	record.MustChangePassword = true
	if err := store.BootstrapLocalIdentity(context.Background(), record); err != nil {
		t.Fatalf("BootstrapLocalIdentity() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}

	var credentialExec *fakeExecCall
	for i := range db.execs {
		if strings.Contains(db.execs[i].query, "INSERT INTO identity_local_credentials") {
			credentialExec = &db.execs[i]
			break
		}
	}
	if credentialExec == nil {
		t.Fatalf("bootstrap did not insert a local identity credential: %#v", db.execs)
	}
	if len(credentialExec.args) != 7 {
		t.Fatalf("credential insert arg count = %d, want 7 (includes must_change_password): %#v", len(credentialExec.args), credentialExec.args)
	}
	if got := credentialExec.args[6]; got != true {
		t.Fatalf("credential insert must_change_password arg = %v, want true", got)
	}
}

// TestBootstrapLocalIdentityGeneratedModeClearsMustChangePassword is the
// regression guard for ESHU_AUTH_BOOTSTRAP_MODE=generated (and invitation
// acceptance, which never sets the field): the credential INSERT must carry
// must_change_password=false when the caller does not opt in, since the
// generated path already achieves effective rotation through the first-run
// setup wizard (#4965).
func TestBootstrapLocalIdentityGeneratedModeClearsMustChangePassword(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(0)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	record := completeBootstrapRecord()
	record.MustChangePassword = false
	if err := store.BootstrapLocalIdentity(context.Background(), record); err != nil {
		t.Fatalf("BootstrapLocalIdentity() error = %v", err)
	}

	var credentialExec *fakeExecCall
	for i := range db.execs {
		if strings.Contains(db.execs[i].query, "INSERT INTO identity_local_credentials") {
			credentialExec = &db.execs[i]
			break
		}
	}
	if credentialExec == nil {
		t.Fatalf("bootstrap did not insert a local identity credential: %#v", db.execs)
	}
	if got := credentialExec.args[6]; got != false {
		t.Fatalf("credential insert must_change_password arg = %v, want false", got)
	}
}

// mustChangePasswordAuthCredentialRow returns the
// selectLocalIdentityCredentialQuery row shape for an admin credential
// flagged must_change_password=true, mirroring the env-seeded bootstrap
// admin's shape (always has an MFA recovery-code factor).
func mustChangePasswordAuthCredentialRow(t *testing.T, password string) []any {
	t.Helper()
	return []any{
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
		true, // must_change_password
	}
}

// TestAuthenticateLocalIdentityMustChangePasswordBlocksSessionAfterValidProof
// proves the forced-rotation gate (issue #4976): a credential proven with the
// correct password AND a valid MFA recovery code still does not receive a
// session when must_change_password=true. This is the acceptance behavior
// #4963 requires for the ESHU_ADMIN_USERNAME/PASSWORD-seeded admin.
func TestAuthenticateLocalIdentityMustChangePasswordBlocksSessionAfterValidProof(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 11, 0, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{mustChangePasswordAuthCredentialRow(t, password)}},
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
	if result.Status != LocalIdentityAuthMustChangePassword || result.Authenticated {
		t.Fatalf("auth result = %#v, want must_change_password without a session", result)
	}
	if result.Auth.SubjectIDHash != "sha256:owner-subject" || !result.Auth.AllScopes {
		t.Fatalf("must_change_password Auth = %#v, want subject/all-scopes populated for the handler", result.Auth)
	}
	// The MFA recovery-code consumption still runs (possession of the second
	// factor must be proven before the caller learns rotation is required),
	// but none of finishLocalIdentityAuthentication's tail writes do: no
	// lockout clear and no bootstrap-credential consume. Exactly one exec
	// (the recovery-code UPDATE) proves the tail never ran.
	if got := len(db.execs); got != 1 {
		t.Fatalf("must_change_password path exec count = %d, want exactly 1 (recovery code consumption only): %#v", got, db.execs)
	}
	if !strings.Contains(db.execs[0].query, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("must_change_password path's one exec = %#v, want recovery code consumption", db.execs[0])
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("must_change_password path issued %d queries, want exactly 1 (credential select only): %#v", got, db.queries)
	}
}

// TestAuthenticateLocalIdentityMustChangePasswordFalseRegressesToAuthenticated
// is the regression guard: a must_change_password=false credential (every
// credential before this issue, and the generated-mode bootstrap admin)
// authenticates exactly as before.
func TestAuthenticateLocalIdentityMustChangePasswordFalseRegressesToAuthenticated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 11, 5, 0, 0, time.UTC)
	password := "correct-password"
	db := localIdentityAuthDB(t, password, []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		"",
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		true,
		true,
		"sha256:policy",
		false, // must_change_password
	})
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
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
}

// TestSelectLocalIdentityCredentialForUpdateQueryLocksCredentialRowOnly
// proves the rotation row-lock query targets only the identity_local_credentials
// alias, not the full join (issue #4976 concurrency requirement).
func TestSelectLocalIdentityCredentialForUpdateQueryLocksCredentialRowOnly(t *testing.T) {
	t.Parallel()

	if !strings.Contains(selectLocalIdentityCredentialForUpdateQuery, "FOR UPDATE OF c") {
		t.Fatalf("selectLocalIdentityCredentialForUpdateQuery missing row lock:\n%s", selectLocalIdentityCredentialForUpdateQuery)
	}
	if !strings.Contains(selectLocalIdentityCredentialForUpdateQuery, "c.must_change_password") {
		t.Fatalf("selectLocalIdentityCredentialForUpdateQuery missing must_change_password column:\n%s", selectLocalIdentityCredentialForUpdateQuery)
	}
}
