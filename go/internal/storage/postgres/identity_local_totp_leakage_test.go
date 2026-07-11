// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/totp"
)

// TestTOTPSecretNeverLeaksAcrossFullEnrollAndLoginCycle is the negative-
// leakage regression for issue #4986, mirroring the #4971 E2E leakage-scan
// shape at the storage layer: it drives a full begin -> confirm -> login
// cycle against the real secretcrypto keyring and totp algorithm (not a
// hand-built stand-in), then scans every captured SQL statement and every
// argument passed to ExecContext/QueryContext for the raw plaintext secret.
// The plaintext must appear in exactly one place — the Seal call inside
// BeginLocalIdentityTOTPEnrollment, which is not observable through the
// ExecQueryer interface this fake records — and nowhere else: not in any
// INSERT/UPDATE/SELECT argument, and not as a substring of the persisted
// sealed envelope (AES-GCM ciphertext cannot equal its own plaintext, but
// this asserts it explicitly rather than assuming the primitive).
func TestTOTPSecretNeverLeaksAcrossFullEnrollAndLoginCycle(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	plaintextSecret := "12345678901234567890"
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	// --- Begin enrollment ---
	beginDB := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(beginDB)
	store.SetTOTPSecretKeyring(keyring)
	if err := store.BeginLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentBegin{
		UserID:          "user_owner",
		FactorID:        "factor_totp_1",
		SecretPlaintext: []byte(plaintextSecret),
		CreatedAt:       now,
	}); err != nil {
		t.Fatalf("BeginLocalIdentityTOTPEnrollment() error = %v", err)
	}
	var sealed string
	for _, exec := range beginDB.execs {
		if strings.Contains(exec.query, "INSERT INTO identity_mfa_factors") {
			sealed, _ = exec.args[2].(string)
		}
	}
	if sealed == "" {
		t.Fatalf("begin did not persist a sealed secret: %#v", beginDB.execs)
	}
	assertNoLeakedSecret(t, "begin", beginDB.execs, beginDB.queries, plaintextSecret)
	if strings.Contains(sealed, plaintextSecret) {
		t.Fatalf("sealed envelope contains the raw plaintext secret: %q", sealed)
	}

	// --- Confirm enrollment (real code, real verify) ---
	code, err := totp.GenerateCode([]byte(plaintextSecret), now, totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	confirmDB := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{sealed}}},
	}}
	store = NewIdentitySubjectStore(confirmDB)
	store.SetTOTPSecretKeyring(keyring)
	if err := store.ConfirmLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentConfirm{
		UserID:   "user_owner",
		FactorID: "factor_totp_1",
		Code:     code,
		Now:      now,
	}); err != nil {
		t.Fatalf("ConfirmLocalIdentityTOTPEnrollment() error = %v", err)
	}
	assertNoLeakedSecret(t, "confirm", confirmDB.execs, confirmDB.queries, plaintextSecret)

	// --- Login (verifyLocalIdentityTOTPCode via AuthenticateLocalIdentity) ---
	loginCode, err := totp.GenerateCode([]byte(plaintextSecret), now.Add(30*time.Second), totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	loginDB := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{adminAuthCredentialRowWithMFA(t, "correct-password")}},
		{rows: [][]any{{"factor_totp_1", sealed}}},
	}}
	store = NewIdentitySubjectStore(loginDB)
	store.SetTOTPSecretKeyring(keyring)
	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:owner-subject",
		Password:      "correct-password",
		MFATOTPCode:   loginCode,
		Now:           now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated {
		t.Fatalf("AuthenticateLocalIdentity() status = %v, want authenticated", result.Status)
	}
	assertNoLeakedSecret(t, "login", loginDB.execs, loginDB.queries, plaintextSecret)
}

// assertNoLeakedSecret fails the test if plaintextSecret appears as a
// substring of any captured SQL statement text or any string-typed argument
// across execs and queries. Non-string arguments (bools, times, ints) can
// never carry the secret and are skipped.
func assertNoLeakedSecret(t *testing.T, phase string, execs []fakeExecCall, queries []fakeQueryCall, plaintextSecret string) {
	t.Helper()
	for _, exec := range execs {
		if strings.Contains(exec.query, plaintextSecret) {
			t.Fatalf("%s: exec SQL text leaked plaintext secret:\n%s", phase, exec.query)
		}
		for i, arg := range exec.args {
			if s, ok := arg.(string); ok && strings.Contains(s, plaintextSecret) {
				t.Fatalf("%s: exec arg[%d] leaked plaintext secret: query=%q arg=%q", phase, i, exec.query, s)
			}
		}
	}
	for _, q := range queries {
		if strings.Contains(q.query, plaintextSecret) {
			t.Fatalf("%s: query SQL text leaked plaintext secret:\n%s", phase, q.query)
		}
		for i, arg := range q.args {
			if s, ok := arg.(string); ok && strings.Contains(s, plaintextSecret) {
				t.Fatalf("%s: query arg[%d] leaked plaintext secret: query=%q arg=%q", phase, i, q.query, s)
			}
		}
	}
}

// TestGetLocalIdentityMFAStatusQueryNeverSelectsSecretColumn is a static
// regression guard: the safe MFA-status read (go/internal/query's profile
// surface, GetLocalIdentityMFAStatus) must never select
// secret_credential_handle, so a future edit cannot accidentally widen it
// into a read surface that leaks the sealed TOTP envelope.
func TestGetLocalIdentityMFAStatusQueryNeverSelectsSecretColumn(t *testing.T) {
	t.Parallel()
	if strings.Contains(getLocalIdentityMFAStatusQuery, "secret_credential_handle") {
		t.Fatalf("getLocalIdentityMFAStatusQuery must never select secret_credential_handle:\n%s", getLocalIdentityMFAStatusQuery)
	}
}
