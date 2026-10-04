// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// Live proof for issue #7499: a wrong-password rotation must complete on a
// single-connection pool. RotateLocalIdentityPassword holds its transaction's
// pooled connection while recording the failed attempt; writing that attempt
// through the pool asks for a second connection, so with MaxOpenConns(1) the
// call waits on itself until the context expires.
func TestRotateLocalIdentityPasswordWrongPasswordSingleConnectionCompletes(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "local_identity_rotate_single_conn", "set ESHU_POSTGRES_DSN to run the rotation single-connection #7499 proof")

	base := time.Now().UTC().Truncate(time.Millisecond)
	userID := "user-live-rotate-single"
	subjectIDHash := "sha256:live-rotate-single"
	seedLiveRotateSingleFixture(t, ctx, database, base, userID, subjectIDHash, "correct-password")

	// The whole store runs on one pooled connection from here on; seeding is
	// finished, so every statement below is the rotation call itself.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := NewIdentitySubjectStore(SQLDB{DB: database})

	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
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

	var failedAttempts int64
	if err := database.QueryRowContext(ctx, `
SELECT failed_attempts
FROM identity_local_auth_attempts
WHERE user_id = $1
`, userID).Scan(&failedAttempts); err != nil {
		t.Fatalf("read failed attempts: %v", err)
	}
	if failedAttempts != 2 {
		t.Fatalf("failed_attempts = %d, want 2 durable wrong-password records", failedAttempts)
	}
}

func seedLiveRotateSingleFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	base time.Time,
	userID, subjectIDHash, password string,
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
}
