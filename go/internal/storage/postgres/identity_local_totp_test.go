// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	"github.com/eshu-hq/eshu/go/internal/totp"
)

func testTOTPKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	kr, err := secretcrypto.NewKeyring("k1", map[secretcrypto.KeyID][]byte{"k1": key})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	return kr
}

func TestBeginLocalIdentityTOTPEnrollment_SealsSecretAndInsertsPendingFactor(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(testTOTPKeyring(t))

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	err := store.BeginLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentBegin{
		UserID:          "user_owner",
		FactorID:        "factor_totp_1",
		SecretPlaintext: []byte("12345678901234567890"),
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("BeginLocalIdentityTOTPEnrollment() error = %v", err)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_mfa_factors") {
		t.Fatalf("execs missing pending totp factor insert: %#v", db.execs)
	}
	var insertedSealed string
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO identity_mfa_factors") {
			insertedSealed, _ = exec.args[2].(string)
		}
	}
	if insertedSealed == "" {
		t.Fatalf("insert args missing sealed secret: %#v", db.execs)
	}
	if strings.Contains(insertedSealed, "12345678901234567890") {
		t.Fatalf("sealed secret leaked plaintext: %q", insertedSealed)
	}
	if !strings.HasPrefix(insertedSealed, "ESK1.") {
		t.Fatalf("sealed secret = %q, want ESK1 envelope", insertedSealed)
	}
}

func TestBeginLocalIdentityTOTPEnrollment_FailsClosedWithoutKeyring(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)
	// No SetTOTPSecretKeyring call.

	err := store.BeginLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentBegin{
		UserID:          "user_owner",
		FactorID:        "factor_totp_1",
		SecretPlaintext: []byte("12345678901234567890"),
		CreatedAt:       time.Now(),
	})
	if !errors.Is(err, ErrLocalIdentityTOTPKeyringUnavailable) {
		t.Fatalf("BeginLocalIdentityTOTPEnrollment() error = %v, want ErrLocalIdentityTOTPKeyringUnavailable", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("expected no writes when keyring unavailable, got %#v", db.execs)
	}
}

func TestBeginLocalIdentityTOTPEnrollment_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeExecQueryer{})
	store.SetTOTPSecretKeyring(testTOTPKeyring(t))

	tests := []struct {
		name  string
		begin LocalIdentityTOTPEnrollmentBegin
	}{
		{"missing user_id", LocalIdentityTOTPEnrollmentBegin{FactorID: "f1", SecretPlaintext: []byte("s"), CreatedAt: time.Now()}},
		{"missing factor_id", LocalIdentityTOTPEnrollmentBegin{UserID: "u1", SecretPlaintext: []byte("s"), CreatedAt: time.Now()}},
		{"missing secret", LocalIdentityTOTPEnrollmentBegin{UserID: "u1", FactorID: "f1", CreatedAt: time.Now()}},
		{"missing created_at", LocalIdentityTOTPEnrollmentBegin{UserID: "u1", FactorID: "f1", SecretPlaintext: []byte("s")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.BeginLocalIdentityTOTPEnrollment(context.Background(), tt.begin); err == nil {
				t.Fatalf("BeginLocalIdentityTOTPEnrollment(%+v) = nil error, want error", tt.begin)
			}
		})
	}
}

// TestConfirmLocalIdentityTOTPEnrollment_ActivatesOnValidCode is an
// end-to-end round trip through the real Seal/Open envelope and the real
// totp.GenerateCode/Verify algorithm (not a hand-built stand-in), proving
// the storage layer and the totp package integrate correctly.
func TestConfirmLocalIdentityTOTPEnrollment_ActivatesOnValidCode(t *testing.T) {
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
		{rows: [][]any{{sealed}}}, // selectLocalIdentityPendingTOTPSecretQuery
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	err = store.ConfirmLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentConfirm{
		UserID:   "user_owner",
		FactorID: "factor_totp_1",
		Code:     code,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("ConfirmLocalIdentityTOTPEnrollment() error = %v", err)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_factors") {
		t.Fatalf("execs missing factor activation: %#v", db.execs)
	}
}

func TestConfirmLocalIdentityTOTPEnrollment_RejectsWrongCode(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{sealed}}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	err = store.ConfirmLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentConfirm{
		UserID:   "user_owner",
		FactorID: "factor_totp_1",
		Code:     "000000",
		Now:      now,
	})
	if !errors.Is(err, ErrLocalIdentityTOTPCodeInvalid) {
		t.Fatalf("ConfirmLocalIdentityTOTPEnrollment() error = %v, want ErrLocalIdentityTOTPCodeInvalid", err)
	}
	if fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_factors") {
		t.Fatalf("factor must not activate on wrong code: %#v", db.execs)
	}
}

func TestConfirmLocalIdentityTOTPEnrollment_NoPendingFactorFailsClosed(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(testTOTPKeyring(t))

	err := store.ConfirmLocalIdentityTOTPEnrollment(context.Background(), LocalIdentityTOTPEnrollmentConfirm{
		UserID:   "user_owner",
		FactorID: "factor_missing",
		Code:     "123456",
		Now:      time.Now(),
	})
	if !errors.Is(err, ErrLocalIdentityTOTPPendingNotFound) {
		t.Fatalf("ConfirmLocalIdentityTOTPEnrollment() error = %v, want ErrLocalIdentityTOTPPendingNotFound", err)
	}
}

func TestVerifyLocalIdentityTOTPCode_MatchesActiveFactorAndStampsLastUsed(t *testing.T) {
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
		{rows: [][]any{{"factor_totp_1", sealed}}}, // selectLocalIdentityActiveTOTPSecretQuery
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	ok, factorID, err := store.verifyLocalIdentityTOTPCode(context.Background(), "user_owner", code, now)
	if err != nil {
		t.Fatalf("verifyLocalIdentityTOTPCode() error = %v", err)
	}
	if !ok || factorID != "factor_totp_1" {
		t.Fatalf("verifyLocalIdentityTOTPCode() = (%v, %q), want (true, factor_totp_1)", ok, factorID)
	}
	if !fakeExecsContainQuery(db.execs, "identity_mfa_factors") {
		t.Fatalf("execs missing last_used_at stamp: %#v", db.execs)
	}
}

func TestVerifyLocalIdentityTOTPCode_WrongCodeReturnsFalseNoError(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	secret := []byte("12345678901234567890")
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD("user_owner", "factor_totp_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{"factor_totp_1", sealed}}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(keyring)

	ok, factorID, err := store.verifyLocalIdentityTOTPCode(context.Background(), "user_owner", "000000", now)
	if err != nil {
		t.Fatalf("verifyLocalIdentityTOTPCode() error = %v", err)
	}
	if ok || factorID != "" {
		t.Fatalf("verifyLocalIdentityTOTPCode() = (%v, %q), want (false, \"\")", ok, factorID)
	}
	if fakeExecsContainQuery(db.execs, "identity_mfa_factors") {
		t.Fatalf("must not stamp last_used_at on wrong code: %#v", db.execs)
	}
}

func TestVerifyLocalIdentityTOTPCode_NoActiveFactorReturnsFalseNoError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{}},
	}}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(testTOTPKeyring(t))

	ok, factorID, err := store.verifyLocalIdentityTOTPCode(context.Background(), "user_owner", "123456", time.Now())
	if err != nil {
		t.Fatalf("verifyLocalIdentityTOTPCode() error = %v", err)
	}
	if ok || factorID != "" {
		t.Fatalf("verifyLocalIdentityTOTPCode() = (%v, %q), want (false, \"\")", ok, factorID)
	}
}

func TestVerifyLocalIdentityTOTPCode_EmptyCodeReturnsFalseNoQuery(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)
	store.SetTOTPSecretKeyring(testTOTPKeyring(t))

	ok, factorID, err := store.verifyLocalIdentityTOTPCode(context.Background(), "user_owner", "", time.Now())
	if err != nil {
		t.Fatalf("verifyLocalIdentityTOTPCode() error = %v", err)
	}
	if ok || factorID != "" {
		t.Fatalf("verifyLocalIdentityTOTPCode() = (%v, %q), want (false, \"\")", ok, factorID)
	}
	if len(db.queries) != 0 {
		t.Fatalf("empty code must not issue a query: %#v", db.queries)
	}
}

// TestTOTPSecretAAD_BindsToUserAndFactor proves an envelope sealed for one
// (user_id, factor_id) fails closed (ErrDecrypt) when opened under a
// different pair's AAD — the cut-and-paste / confused-deputy defense
// documented in secretcrypto's README.
func TestTOTPSecretAAD_BindsToUserAndFactor(t *testing.T) {
	t.Parallel()

	keyring := testTOTPKeyring(t)
	sealed, err := keyring.Seal([]byte("shhh"), []byte(totpSecretAAD("user_a", "factor_1")))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := keyring.Open(sealed, []byte(totpSecretAAD("user_b", "factor_1"))); !errors.Is(err, secretcrypto.ErrDecrypt) {
		t.Fatalf("Open() with wrong user_id error = %v, want ErrDecrypt", err)
	}
	if _, err := keyring.Open(sealed, []byte(totpSecretAAD("user_a", "factor_2"))); !errors.Is(err, secretcrypto.ErrDecrypt) {
		t.Fatalf("Open() with wrong factor_id error = %v, want ErrDecrypt", err)
	}
	plaintext, err := keyring.Open(sealed, []byte(totpSecretAAD("user_a", "factor_1")))
	if err != nil {
		t.Fatalf("Open() with correct AAD error = %v", err)
	}
	if string(plaintext) != "shhh" {
		t.Fatalf("Open() plaintext = %q, want %q", plaintext, "shhh")
	}
}
