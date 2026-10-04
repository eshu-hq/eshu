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

// Live proof for issue #7499: every rotation path must complete on a
// single-connection pool. RotateLocalIdentityPassword holds its transaction's
// pooled connection; any statement that went through the pool instead of tx
// would ask for a second connection, so with MaxOpenConns(1) the call waits
// on itself until the context expires. Each sub-proof below runs on the same
// one-connection pool: wrong password, wrong TOTP with an active factor,
// an invalid recovery code, and a full success rotation with TOTP re-proof.
func TestRotateLocalIdentityPasswordSingleConnectionCompletes(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "local_identity_rotate_single_conn", "set ESHU_POSTGRES_DSN to run the rotation single-connection #7499 proof")

	base := time.Now().UTC().Truncate(time.Millisecond)
	userID := "user-live-rotate-single"
	subjectIDHash := "sha256:live-rotate-single"
	factorID := "factor-live-rotate-single"
	secret := []byte("12345678901234567890")
	keyring := testTOTPKeyring(t)
	sealed, err := keyring.Seal(secret, []byte(totpSecretAAD(userID, factorID)))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	seedLiveRotateSingleFixture(t, ctx, database, base, userID, subjectIDHash, "correct-password", factorID, sealed)

	// The whole store runs on one pooled connection from here on; seeding is
	// finished, so every statement below is the rotation call itself.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := NewIdentitySubjectStore(SQLDB{DB: database})
	store.SetTOTPSecretKeyring(keyring)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		result, err := store.RotateLocalIdentityPassword(callCtx, LocalIdentityPasswordRotation{
			SubjectIDHash:             subjectIDHash,
			CurrentPassword:           "WRONG-password",
			NewPasswordHash:           "bcrypt:new-hash",
			NewPasswordAlgorithm:      "bcrypt",
			NewPasswordParametersHash: "sha256:bcrypt-cost",
			CredentialID:              "credential:live-rotate-single:rotated",
			Now:                       base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("RotateLocalIdentityPassword() wrong-password attempt %d error = %v", i+1, err)
		}
		if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
			t.Fatalf("wrong-password attempt %d result = %#v, want invalid without a session", i+1, result)
		}
	}

	if got := readLiveFailedAttempts(t, ctx, database, userID); got != 2 {
		t.Fatalf("failed_attempts = %d, want 2 durable wrong-password records", got)
	}

	// Wrong TOTP with the correct password: the TOTP read and the failed
	// attempt both run on tx, so this also completes on one connection.
	result, err := store.RotateLocalIdentityPassword(callCtx, LocalIdentityPasswordRotation{
		SubjectIDHash:             subjectIDHash,
		CurrentPassword:           "correct-password",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:live-rotate-single:rotated",
		MFATOTPCode:               "000000",
		Now:                       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() wrong-totp error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("wrong-totp result = %#v, want invalid without a session", result)
	}
	if got := readLiveFailedAttempts(t, ctx, database, userID); got != 3 {
		t.Fatalf("failed_attempts = %d, want 3 durable records after wrong totp", got)
	}

	// Invalid recovery code with the correct password: the consume attempt
	// matches no row, so the invalid leg records the failed attempt on tx and
	// this also completes on one connection. No recovery-code seeding needed.
	result, err = store.RotateLocalIdentityPassword(callCtx, LocalIdentityPasswordRotation{
		SubjectIDHash:             subjectIDHash,
		CurrentPassword:           "correct-password",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:live-rotate-single:rotated",
		MFARecoveryCodeHash:       "sha256:live-rotate-single-wrong-recovery",
		Now:                       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() wrong-recovery error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("wrong-recovery result = %#v, want invalid without a session", result)
	}
	if got := readLiveFailedAttempts(t, ctx, database, userID); got != 4 {
		t.Fatalf("failed_attempts = %d, want 4 durable records after wrong recovery code", got)
	}

	// Full success rotation with TOTP re-proof: revoke + insert + TOTP stamp
	// commit on tx, then the pool-based tail runs sequentially after commit.
	now := time.Now().UTC()
	code, err := totp.GenerateCode(secret, now, totp.DefaultStep, totp.DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	result, err = store.RotateLocalIdentityPassword(callCtx, LocalIdentityPasswordRotation{
		SubjectIDHash:             subjectIDHash,
		CurrentPassword:           "correct-password",
		NewPasswordHash:           mustBcryptHash(t, "new-correct-password"),
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:live-rotate-single:rotated",
		MFATOTPCode:               code,
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() success error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("success result = %#v, want authenticated", result)
	}
	if got := readLiveFailedAttempts(t, ctx, database, userID); got != 0 {
		t.Fatalf("failed_attempts = %d, want 0 after successful rotation clears lockout", got)
	}
}

func readLiveFailedAttempts(t *testing.T, ctx context.Context, database *sql.DB, userID string) int64 {
	t.Helper()
	var failedAttempts int64
	if err := database.QueryRowContext(ctx, `
SELECT COALESCE((
  SELECT failed_attempts
  FROM identity_local_auth_attempts
  WHERE user_id = $1
), 0)
`, userID).Scan(&failedAttempts); err != nil {
		t.Fatalf("read failed attempts: %v", err)
	}
	return failedAttempts
}

func seedLiveRotateSingleFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	base time.Time,
	userID, subjectIDHash, password, factorID, sealed string,
) {
	t.Helper()
	tenantID := "tenant-live-rotate-single"
	workspaceID := "workspace-live-rotate-single"
	if _, err := database.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'active', '', 'sha256:policy', $2, $2, NULL)
ON CONFLICT (tenant_id) DO NOTHING
`, tenantID, base); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, $2, 'active', '', 'sha256:policy', $3, $3, NULL)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING
`, tenantID, workspaceID, base); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, profile_handle_hash, created_at, updated_at, disabled_at, tombstoned_at)
VALUES ($1, $2, 'active', '', $3, $3, NULL, NULL)
`, userID, subjectIDHash, base); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_local_credentials (credential_id, user_id, password_hash, password_algorithm, password_parameters_hash, status, created_at, rotated_at, expires_at, revoked_at, must_change_password)
VALUES ($1, $2, $3, 'bcrypt', 'sha256:bcrypt-cost', 'active', $4, $4, NULL, NULL, false)
`, userID+"-cred", userID, mustBcryptHash(t, password), base); err != nil {
		t.Fatalf("seed local credential: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_tenant_memberships (tenant_id, workspace_id, user_id, status, membership_source, policy_revision_hash, effective_at, expires_at, disabled_at, tombstoned_at, created_at, updated_at)
VALUES ($1, $2, $3, 'active', 'bootstrap', 'sha256:policy', $4, NULL, NULL, NULL, $4, $4)
`, tenantID, workspaceID, userID, base); err != nil {
		t.Fatalf("seed tenant membership: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_mfa_factors (factor_id, user_id, factor_kind, status, secret_credential_handle, created_at, verified_at, last_used_at, revoked_at)
VALUES ($1, $2, 'totp', 'active', $3, $4, $4, NULL, NULL)
`, factorID, userID, sealed, base); err != nil {
		t.Fatalf("seed totp factor: %v", err)
	}
	// Owner role so the success rotation takes the all-scope tail instead of
	// resolving a permission-catalog snapshot for a fixture with no grants.
	// identity_membership_roles has an FK to identity_roles, so the parent
	// role row comes first (same column shape as upsertLocalIdentityRoleQuery).
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_roles (tenant_id, role_id, role_key_hash, status, built_in, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'owner', $2, 'active', true, 'sha256:policy', $3, $3, NULL)
ON CONFLICT (tenant_id, role_id) DO NOTHING
`, tenantID, localIdentityRoleKeyHash("owner"), base); err != nil {
		t.Fatalf("seed owner role row: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_membership_roles (tenant_id, workspace_id, user_id, role_id, assignment_source, status, policy_revision_hash, effective_at, expires_at, tombstoned_at, created_at, updated_at)
VALUES ($1, $2, $3, 'owner', 'bootstrap', 'active', 'sha256:policy', $4, NULL, NULL, $4, $4)
`, tenantID, workspaceID, userID, base); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}
}
