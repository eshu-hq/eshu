// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestGenerateSecretIsRandomAndSized(t *testing.T) {
	a, err := generateSecret(24)
	if err != nil {
		t.Fatalf("generateSecret() error = %v", err)
	}
	b, err := generateSecret(24)
	if err != nil {
		t.Fatalf("generateSecret() error = %v", err)
	}
	if a == b {
		t.Fatal("generateSecret() produced identical values across calls")
	}
	if a == "" || b == "" {
		t.Fatal("generateSecret() produced an empty value")
	}
}

// TestOpenBootstrapCredentialPayloadDecryptFailureIsActionable proves the CLI
// converts a raw secretcrypto.ErrDecrypt (wrong DEK) into the documented
// actionable guidance, never a bare/opaque error.
func TestOpenBootstrapCredentialPayloadDecryptFailureIsActionable(t *testing.T) {
	sealingKeyring, err := secretcrypto.NewKeyring("key-a", map[secretcrypto.KeyID][]byte{"key-a": bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	wrongKeyring, err := secretcrypto.NewKeyring("key-b", map[secretcrypto.KeyID][]byte{"key-b": bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}

	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := sealingKeyring.Seal([]byte(`{"username":"admin","password":"x","recovery_code":"y"}`), aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	db := &fakeAdminCredDB{sealed: sealed, keyID: "key-a", found: true}
	store := pgstorage.NewIdentitySubjectStore(db)

	_, gotKeyID, err := openBootstrapCredentialPayload(context.Background(), store, wrongKeyring)
	if err == nil {
		t.Fatal("openBootstrapCredentialPayload() error = nil, want decrypt-failure guidance")
	}
	if !strings.Contains(err.Error(), "reset-initial-credential") {
		t.Fatalf("error = %q, want actionable reset guidance", err.Error())
	}
	// The envelope's own key_id is known from the row before Open ever runs;
	// a failed-retrieval audit event should still be able to correlate
	// against it (which DEK the caller needed but didn't have).
	if gotKeyID != "key-a" {
		t.Fatalf("openBootstrapCredentialPayload() keyID = %q, want %q even on decrypt failure", gotKeyID, "key-a")
	}
}

func TestOpenBootstrapCredentialPayloadNotFoundIsActionable(t *testing.T) {
	keyring, err := secretcrypto.NewKeyring("key-a", map[secretcrypto.KeyID][]byte{"key-a": bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	db := &fakeAdminCredDB{found: false}
	store := pgstorage.NewIdentitySubjectStore(db)

	_, _, err = openBootstrapCredentialPayload(context.Background(), store, keyring)
	if err == nil {
		t.Fatal("openBootstrapCredentialPayload() error = nil, want not-found guidance")
	}
	if !strings.Contains(err.Error(), "reset-initial-credential") {
		t.Fatalf("error = %q, want actionable reset guidance", err.Error())
	}
}

// TestAdminInitialCredentialAndResetRoundTrip is a real-Postgres proof: seed
// a generated bootstrap credential, retrieve it via `initial-credential`,
// reset it via `reset-initial-credential`, and prove the retrieved values
// change and the local password hash rotates. Skipped unless a DSN is
// provided, matching the storage package's other real-Postgres proofs.
func TestAdminInitialCredentialAndResetRoundTrip(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the admin initial-credential CLI round trip")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("admin_initial_credential_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := pgstorage.ApplyBootstrap(ctx, pgstorage.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64 of 32 raw bytes
	t.Setenv("ESHU_POSTGRES_DSN", dsn+"?search_path="+schemaName)
	t.Setenv("ESHU_AUTH_SECRET_ENC_KEY", dek)

	now := time.Now().UTC()
	userID := "user-cli-round-trip"
	subjectIDHash := "sha256:cli-round-trip-subject"
	originalRecoveryCode := "original-first-run-recovery-code"
	seedIdentityFixture(t, ctx, db, userID, subjectIDHash, originalRecoveryCode, now)

	// Seed an ACTIVE TOTP factor the admin enrolled after bootstrap, the
	// documented invariant this reset must never touch. A raw fixture insert
	// (rather than driving the full totp-package enroll/confirm code
	// exchange) is deliberate: it proves ResetBootstrapCredential's SQL
	// scoping directly against a real row, independent of whether the totp
	// package itself is exercised elsewhere.
	totpFactorID := "id_cli-round-trip-totp"
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_mfa_factors (factor_id, user_id, factor_kind, status, secret_credential_handle, public_key_hash, created_at, verified_at, last_used_at, revoked_at)
VALUES ($1, $2, 'totp', 'active', 'sha256:totp-handle', NULL, $3, $3, NULL, NULL)
`, totpFactorID, userID, now); err != nil {
		t.Fatalf("seed totp factor fixture: %v", err)
	}

	keyring, err := secretcrypto.KeyringFromEnv(func(k string) string {
		if k == "ESHU_AUTH_SECRET_ENC_KEY" {
			return dek
		}
		return ""
	})
	if err != nil {
		t.Fatalf("KeyringFromEnv() error = %v", err)
	}
	store := pgstorage.NewIdentitySubjectStore(pgstorage.SQLDB{DB: db})
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal([]byte(`{"username":"admin","password":"initial-pw","recovery_code":"initial-rc"}`), aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	keyID := secretcrypto.EnvelopeKeyID(sealed)
	if keyID == "" {
		t.Fatalf("EnvelopeKeyID() returned empty for a freshly sealed envelope: %q", sealed)
	}
	inserted, err := store.GenerateBootstrapCredential(ctx, pgstorage.BootstrapCredentialSeal{
		TenantID:         pgstorage.BootstrapAdminTenantID,
		WorkspaceID:      pgstorage.BootstrapAdminWorkspaceID,
		SubjectIDHash:    subjectIDHash,
		UsernameHash:     "sha256:admin-username",
		SealedCredential: sealed,
		KeyID:            keyID,
		GeneratedAt:      now,
	})
	if err != nil || !inserted {
		t.Fatalf("GenerateBootstrapCredential() inserted=%t err=%v", inserted, err)
	}

	// initial-credential retrieves the seeded plaintext bundle.
	var initialOut bytes.Buffer
	payload, retrievedKeyID, err := openBootstrapCredentialPayload(ctx, store, keyring)
	if err != nil {
		t.Fatalf("openBootstrapCredentialPayload() error = %v", err)
	}
	if retrievedKeyID != keyID {
		t.Fatalf("openBootstrapCredentialPayload() keyID = %q, want %q", retrievedKeyID, keyID)
	}
	fmt.Fprintf(&initialOut, "username:      %s\npassword:      %s\nrecovery code: %s\n",
		payload.Username, payload.Password, payload.RecoveryCode)
	if !strings.Contains(initialOut.String(), "initial-pw") {
		t.Fatalf("initial-credential output missing seeded password: %q", initialOut.String())
	}

	// reset-initial-credential regenerates and rotates atomically.
	newPassword, err := generateSecret(generatedPasswordSize)
	if err != nil {
		t.Fatalf("generateSecret() error = %v", err)
	}
	newRecovery, err := generateSecret(generatedRecoverySize)
	if err != nil {
		t.Fatalf("generateSecret() error = %v", err)
	}
	newHashBytes, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	newHash := string(newHashBytes)
	newPayload, err := json.Marshal(bootstrapCredentialPayloadCLI{
		Username: "admin", Password: newPassword, RecoveryCode: newRecovery,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	newSealed, err := keyring.Seal(newPayload, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	newKeyID := secretcrypto.EnvelopeKeyID(newSealed)
	if newKeyID == "" {
		t.Fatalf("EnvelopeKeyID() returned empty for a freshly sealed envelope: %q", newSealed)
	}
	newMFAFactorID, err := newLocalIdentityFactorID()
	if err != nil {
		t.Fatalf("newLocalIdentityFactorID() error = %v", err)
	}
	newRecoveryCodeHash := query.IdentityHash(newRecovery)
	if err := store.ResetBootstrapCredential(ctx, pgstorage.ResetBootstrapCredentialInput{
		TenantID:               pgstorage.BootstrapAdminTenantID,
		WorkspaceID:            pgstorage.BootstrapAdminWorkspaceID,
		SealedCredential:       newSealed,
		KeyID:                  newKeyID,
		PasswordHash:           newHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: query.IdentityHash("bcrypt"),
		MFAFactorID:            newMFAFactorID,
		RecoveryCodeHash:       newRecoveryCodeHash,
		ResetAt:                time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ResetBootstrapCredential() error = %v", err)
	}

	// Retrieval after reset returns the NEW bundle, not the original.
	afterReset, afterResetKeyID, err := openBootstrapCredentialPayload(ctx, store, keyring)
	if err != nil {
		t.Fatalf("openBootstrapCredentialPayload() after reset error = %v", err)
	}
	if afterResetKeyID != newKeyID {
		t.Fatalf("post-reset openBootstrapCredentialPayload() keyID = %q, want %q", afterResetKeyID, newKeyID)
	}
	if afterReset.Password != newPassword {
		t.Fatalf("post-reset password = %q, want %q", afterReset.Password, newPassword)
	}
	if afterReset.Password == "initial-pw" {
		t.Fatal("post-reset retrieval still returned the pre-reset password")
	}

	// The bcrypt hash in identity_local_credentials rotated to match.
	var storedHash string
	row := db.QueryRowContext(ctx, `SELECT password_hash FROM identity_local_credentials WHERE user_id = $1 AND status = 'active' AND revoked_at IS NULL`, userID)
	if err := row.Scan(&storedHash); err != nil {
		t.Fatalf("read rotated password hash: %v", err)
	}
	if storedHash != newHash {
		t.Fatalf("stored password_hash = %q, want %q (reset did not rotate identity_local_credentials)", storedHash, newHash)
	}

	// The TOTP factor seeded above must be completely untouched: same status,
	// same revoked_at, same last_used_at. ResetBootstrapCredential's recovery-
	// factor revocation is scoped to factor_kind='recovery_code' — this is the
	// live-Postgres proof of that scoping, not just a read of the SQL text.
	var (
		totpStatus     string
		totpRevokedAt  sql.NullTime
		totpLastUsedAt sql.NullTime
	)
	row2 := db.QueryRowContext(ctx, `SELECT status, revoked_at, last_used_at FROM identity_mfa_factors WHERE factor_id = $1`, totpFactorID)
	if err := row2.Scan(&totpStatus, &totpRevokedAt, &totpLastUsedAt); err != nil {
		t.Fatalf("read totp factor row after reset: %v", err)
	}
	if totpStatus != "active" || totpRevokedAt.Valid {
		t.Fatalf(
			"reset touched the TOTP factor it must never touch: status=%q revoked_at.Valid=%t (want active, revoked_at NULL)",
			totpStatus, totpRevokedAt.Valid,
		)
	}
	if totpLastUsedAt.Valid {
		t.Fatalf("reset modified the TOTP factor's last_used_at, want untouched NULL: %v", totpLastUsedAt.Time)
	}

	// The core proof for issue #5602: the recovery code this command PRINTS
	// after a reset must actually authenticate, and the code it replaced must
	// not. Neither check below drives AuthenticateLocalIdentity into a FAILED
	// attempt (the no-MFA-proof call short-circuits to mfa_required before
	// any lockout bookkeeping runs), so this never touches account lockout.
	noMFA, err := store.AuthenticateLocalIdentity(ctx, pgstorage.LocalIdentityAuthenticationAttempt{
		SubjectIDHash: subjectIDHash,
		Password:      newPassword,
		Now:           time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() (no MFA proof) error = %v", err)
	}
	if noMFA.Status != pgstorage.LocalIdentityAuthMFARequired {
		t.Fatalf("post-reset password-only login status = %q, want %q", noMFA.Status, pgstorage.LocalIdentityAuthMFARequired)
	}

	// Prove the ORIGINAL recovery code row was revoked directly against the
	// table AuthenticateLocalIdentity reads (rather than by driving another
	// failed AuthenticateLocalIdentity call): a failed-login attempt also
	// exercises identity_local_auth_attempts' lockout bookkeeping, an
	// unrelated code path with its own pgx NULL-timestamp binding issue this
	// test must not couple to.
	originalRecoveryCodeHash := query.IdentityHash(originalRecoveryCode)
	var originalCodeStatus string
	row = db.QueryRowContext(ctx, `SELECT status FROM identity_mfa_recovery_codes WHERE user_id = $1 AND recovery_code_hash = $2`, userID, originalRecoveryCodeHash)
	if err := row.Scan(&originalCodeStatus); err != nil {
		t.Fatalf("read original recovery code row: %v", err)
	}
	if originalCodeStatus != "revoked" {
		t.Fatalf("original recovery code status = %q, want %q (reset must revoke the code it replaces)", originalCodeStatus, "revoked")
	}

	freshAttempt, err := store.AuthenticateLocalIdentity(ctx, pgstorage.LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       subjectIDHash,
		Password:            newPassword,
		MFARecoveryCodeHash: newRecoveryCodeHash,
		Now:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() (fresh recovery code) error = %v", err)
	}
	if !freshAttempt.Authenticated || freshAttempt.Status != pgstorage.LocalIdentityAuthAuthenticated {
		t.Fatalf(
			"the recovery code printed by reset-initial-credential did not authenticate (issue #5602): result = %#v",
			freshAttempt,
		)
	}
}

// seedIdentityFixture seeds a real bootstrap admin identity through
// store.BootstrapLocalIdentity — the same production path
// go/cmd/api/seed_initial_admin.go uses — rather than hand-built SQL, so the
// resulting row set includes the owner role/membership grants and the
// original recovery-code MFA factor AuthenticateLocalIdentity actually reads.
// recoveryCode is the plaintext of the ORIGINAL (pre-reset) recovery code;
// the test hashes it to prove the reset revokes it.
func seedIdentityFixture(
	t *testing.T, ctx context.Context, db *sql.DB, userID, subjectIDHash, recoveryCode string, now time.Time,
) {
	t.Helper()
	store := pgstorage.NewIdentitySubjectStore(pgstorage.SQLDB{DB: db})
	mfaFactorID, err := newLocalIdentityFactorID()
	if err != nil {
		t.Fatalf("newLocalIdentityFactorID() error = %v", err)
	}
	err = store.BootstrapLocalIdentity(ctx, pgstorage.LocalIdentityBootstrapRecord{
		TenantID:               pgstorage.BootstrapAdminTenantID,
		WorkspaceID:            pgstorage.BootstrapAdminWorkspaceID,
		UserID:                 userID,
		SubjectIDHash:          subjectIDHash,
		ProfileHandleHash:      "sha256:cli-round-trip-handle",
		PasswordHash:           "bcrypt:initial-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		MFAFactorID:            mfaFactorID,
		MFAFactorKind:          "recovery_code",
		RecoveryCodeHashes:     []string{query.IdentityHash(recoveryCode)},
		PolicyRevisionHash:     "sha256:policy",
		CreatedAt:              now,
		MustChangePassword:     false,
	})
	if err != nil {
		t.Fatalf("BootstrapLocalIdentity() error = %v", err)
	}
}

// fakeAdminCredDB is a minimal pgstorage.ExecQueryer for unit-testing
// openBootstrapCredentialPayload without a real Postgres connection.
type fakeAdminCredDB struct {
	sealed string
	keyID  string
	found  bool
}

func (f *fakeAdminCredDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected ExecContext call")
}

func (f *fakeAdminCredDB) QueryContext(_ context.Context, _ string, _ ...any) (pgstorage.Rows, error) {
	if !f.found {
		return &fakeAdminCredRows{}, nil
	}
	return &fakeAdminCredRows{rows: [][]any{{f.sealed, f.keyID}}}, nil
}

type fakeAdminCredRows struct {
	rows  [][]any
	index int
}

func (r *fakeAdminCredRows) Next() bool { return r.index < len(r.rows) }

func (r *fakeAdminCredRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		*dest[i].(*string) = row[i].(string)
	}
	r.index++
	return nil
}

func (r *fakeAdminCredRows) Err() error   { return nil }
func (r *fakeAdminCredRows) Close() error { return nil }
