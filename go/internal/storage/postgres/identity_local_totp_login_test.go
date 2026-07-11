// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/totp"
)

// adminAuthCredentialRowWithMFA builds the selectLocalIdentityCredentialQuery
// row shape for an admin/owner local user with an active MFA factor, used by
// the TOTP login-path integration tests below (issue #4986).
func adminAuthCredentialRowWithMFA(t *testing.T, password string) []any {
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
		false, // must_change_password
	}
}

func TestAuthenticateLocalIdentityAcceptsValidTOTPCode(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	code, err := totp.GenerateCode(secret, now, totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}}, // selectLocalIdentityCredentialQuery
		{rows: [][]any{{"factor_totp_1", sealed}}},                            // selectLocalIdentityActiveTOTPSecretQuery
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:owner-subject",
		Password:      "correct-password",
		MFATOTPCode:   code,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
	if !fakeExecsContainQuery(db.execs, "identity_mfa_factors") {
		t.Fatalf("execs missing totp last_used_at stamp: %#v", db.execs)
	}
	if fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("recovery code must not be consumed on a TOTP-verified login: %#v", db.execs)
	}
	for _, exec := range db.execs {
		if fakeExecArgsContain(exec.args, code) {
			t.Fatalf("auth args leaked raw totp code: %#v", exec.args)
		}
	}
}

func TestAuthenticateLocalIdentityRejectsWrongTOTPCode(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}},
		{rows: [][]any{{"factor_totp_1", sealed}}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:owner-subject",
		Password:      "correct-password",
		MFATOTPCode:   "000000",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("auth result = %#v, want invalid (failed attempt recorded)", result)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_auth_attempts") {
		t.Fatalf("auth execs missing failed-attempt upsert: %#v", db.execs)
	}
}

func TestAuthenticateLocalIdentityPrefersTOTPOverRecoveryCodeWhenBothSubmitted(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	code, err := totp.GenerateCode(secret, now, totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}},
		{rows: [][]any{{"factor_totp_1", sealed}}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:owner-subject",
		Password:            "correct-password",
		MFATOTPCode:         code,
		MFARecoveryCodeHash: "sha256:recovery-a", // must be ignored: TOTP proof takes priority
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}
	if fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("recovery code must not be consumed when a valid TOTP code was also submitted: %#v", db.execs)
	}
}

func TestAuthenticateLocalIdentityFallsBackToRecoveryCodeWhenNoTOTPSubmitted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", adminAuthCredentialRowWithMFA(t, "correct-password"))
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
	if result.Status != LocalIdentityAuthAuthenticated {
		t.Fatalf("auth result = %#v, want authenticated via recovery code", result)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("recovery code path must still work when no totp code is submitted: %#v", db.execs)
	}
}

func TestRotateLocalIdentityPasswordAcceptsValidTOTPCode(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	code, err := totp.GenerateCode(secret, now, totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}}, // selectLocalIdentityCredentialForUpdateQuery
			{rows: [][]any{{"factor_totp_1", sealed}}},                            // selectLocalIdentityActiveTOTPSecretQuery
		}},
	}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           "correct-password",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		MFATOTPCode:               code,
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("rotation result = %#v, want authenticated", result)
	}
	if !db.committed {
		t.Fatalf("expected rotation transaction to commit, rolledBack=%t", db.rolledBack)
	}
}

func TestRotateLocalIdentityPasswordRejectsWrongTOTPCode(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}},
			{rows: [][]any{{"factor_totp_1", sealed}}},
		}},
	}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           "correct-password",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		MFATOTPCode:               "000000",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("rotation result = %#v, want invalid", result)
	}
	if db.committed {
		t.Fatalf("rotation must not commit on wrong totp code")
	}
}
