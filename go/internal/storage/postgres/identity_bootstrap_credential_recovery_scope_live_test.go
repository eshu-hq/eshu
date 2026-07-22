// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestReenrollBootstrapCredentialRecoveryFactorNeverRevokesTOTPRecoveryCodes
// is the real-Postgres proof for issue #5602's codex P1 finding:
// reenrollBootstrapCredentialRecoveryFactor (identity_bootstrap_credential_mfa.go)
// must revoke only the recovery codes owned by the recovery_code-kind factor,
// never codes a TOTP-kind factor happens to own.
//
// A TOTP-kind factor can legitimately own rows in identity_mfa_recovery_codes:
// insertLocalIdentityMFA (identity_local_helpers.go) is a shared helper that
// ResetLocalIdentityMFA (identity_local_lifecycle.go, the general
// operator-facing MFA reset) also calls with an operator-supplied
// mfa_factor_kind alongside recovery codes — the admin MFA-reset HTTP
// endpoint accepts an arbitrary mfa_factor_kind field
// (internal/query/local_identity_requests.go's `json:"mfa_factor_kind"`) with
// no kind/pairing validation. Before the fix, reenrollBootstrapCredentialRecoveryFactor
// revoked EVERY active row in identity_mfa_recovery_codes for the user via
// the unscoped revokeLocalIdentityRecoveryCodesQuery, so restoring the
// bootstrap credential would silently destroy that TOTP factor's backup
// codes. This test seeds exactly that scenario against real Postgres and
// proves the fix's factor_kind-scoped query leaves the TOTP-owned code
// untouched while still rotating the recovery_code-owned one.
//
// Run with: ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu
//
//	go test ./internal/storage/postgres -run 'ReenrollBootstrapCredentialRecoveryFactorNeverRevokesTOTP' -count=1 -v
func TestReenrollBootstrapCredentialRecoveryFactorNeverRevokesTOTPRecoveryCodes(t *testing.T) {
	dsn := bootstrapCredentialRecoveryScopeLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres bootstrap-credential recovery-code scoping proof")
	}

	ctx := context.Background()
	db := openBootstrapCredentialRecoveryScopeLiveSchema(t, ctx, dsn)

	const userID = "user_bootstrap_owner"
	const staleRecoveryFactorID = "factor_recovery_stale"
	const totpFactorID = "factor_totp_active"
	const newRecoveryFactorID = "factor_recovery_fresh"
	const staleRecoveryCodeHash = "sha256:stale-recovery-code"
	const totpRecoveryCodeHash = "sha256:totp-owned-recovery-code"
	const newRecoveryCodeHash = "sha256:fresh-recovery-code"

	seededAt := time.Now().UTC().Add(-time.Hour)
	seedBootstrapCredentialRecoveryScopeLiveUser(t, ctx, db, userID, seededAt)
	// The stale recovery_code-kind factor and its code — what a prior
	// bootstrap enrollment or reset left behind.
	seedBootstrapCredentialRecoveryScopeLiveFactor(t, ctx, db, staleRecoveryFactorID, userID, "recovery_code", seededAt)
	seedBootstrapCredentialRecoveryScopeLiveRecoveryCode(t, ctx, db, userID, staleRecoveryFactorID, staleRecoveryCodeHash, seededAt)
	// A TOTP-kind factor that ALSO owns a recovery-code row — reproducing
	// ResetLocalIdentityMFA's admin path, which accepts an arbitrary
	// mfa_factor_kind alongside recovery codes and stores them together via
	// the shared insertLocalIdentityMFA helper.
	seedBootstrapCredentialRecoveryScopeLiveFactor(t, ctx, db, totpFactorID, userID, "totp", seededAt)
	seedBootstrapCredentialRecoveryScopeLiveRecoveryCode(t, ctx, db, userID, totpFactorID, totpRecoveryCodeHash, seededAt)

	resetAt := time.Now().UTC()
	tx, err := SQLDB{DB: db}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reset transaction: %v", err)
	}
	if err := reenrollBootstrapCredentialRecoveryFactor(
		ctx, tx, userID, newRecoveryFactorID, newRecoveryCodeHash, resetAt,
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("reenrollBootstrapCredentialRecoveryFactor() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit reset transaction: %v", err)
	}

	totpCode := selectBootstrapCredentialRecoveryScopeLiveCodeState(t, ctx, db, userID, totpRecoveryCodeHash)
	if totpCode.status != "active" || totpCode.revokedAt.Valid {
		t.Fatalf(
			"TOTP-owned recovery code was touched by the bootstrap credential reset: status=%q revokedAt.Valid=%t — "+
				"the recovery-code revocation must scope to recovery_code-kind factors only",
			totpCode.status, totpCode.revokedAt.Valid,
		)
	}

	staleCode := selectBootstrapCredentialRecoveryScopeLiveCodeState(t, ctx, db, userID, staleRecoveryCodeHash)
	if staleCode.status != "revoked" || !staleCode.revokedAt.Valid {
		t.Fatalf(
			"stale recovery_code-owned code was NOT revoked: status=%q revokedAt.Valid=%t, want revoked",
			staleCode.status, staleCode.revokedAt.Valid,
		)
	}

	freshCode := selectBootstrapCredentialRecoveryScopeLiveCodeState(t, ctx, db, userID, newRecoveryCodeHash)
	if freshCode.status != "active" || freshCode.revokedAt.Valid {
		t.Fatalf(
			"freshly re-enrolled recovery code is not active: status=%q revokedAt.Valid=%t",
			freshCode.status, freshCode.revokedAt.Valid,
		)
	}

	totpFactorStatus := selectBootstrapCredentialRecoveryScopeLiveFactorStatus(t, ctx, db, totpFactorID)
	if totpFactorStatus != "active" {
		t.Fatalf("TOTP factor was revoked by the bootstrap credential reset: status=%q, want active", totpFactorStatus)
	}
	staleFactorStatus := selectBootstrapCredentialRecoveryScopeLiveFactorStatus(t, ctx, db, staleRecoveryFactorID)
	if staleFactorStatus != "revoked" {
		t.Fatalf("stale recovery_code factor was not revoked: status=%q, want revoked", staleFactorStatus)
	}
}

func bootstrapCredentialRecoveryScopeLiveProofDSN() string {
	return os.Getenv("ESHU_POSTGRES_DSN")
}

// openBootstrapCredentialRecoveryScopeLiveSchema creates an isolated
// throwaway schema and applies exactly the migrations
// reenrollBootstrapCredentialRecoveryFactor depends on: ingestion_scopes (an
// FK target of tenant_workspace_grants), tenant_workspace_grants (tenants and
// workspaces, referenced by other tables in the identity_subjects
// migration), and the identity subject tables themselves
// (identity_users, identity_mfa_factors, identity_mfa_recovery_codes) —
// mirroring openProviderConfigLiveSchema (identity_provider_config_live_test.go),
// this package's established live-Postgres proof pattern.
func openBootstrapCredentialRecoveryScopeLiveSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("bootstrap_cred_recovery_scope_live_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create bootstrap credential recovery scope live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, defName := range []string{"ingestion_scopes", "tenant_workspace_grants", "identity_subjects"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(defName)); err != nil {
			t.Fatalf("apply migration %q: %v", defName, err)
		}
	}
	return db
}

func seedBootstrapCredentialRecoveryScopeLiveUser(t *testing.T, ctx context.Context, db *sql.DB, userID string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, created_at, updated_at)
VALUES ($1, $2, 'active', $3, $3)`, userID, "sha256:subject-"+userID, at); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
}

func seedBootstrapCredentialRecoveryScopeLiveFactor(
	t *testing.T, ctx context.Context, db *sql.DB, factorID, userID, factorKind string, at time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_mfa_factors (factor_id, user_id, factor_kind, status, created_at, verified_at)
VALUES ($1, $2, $3, 'active', $4, $4)`, factorID, userID, factorKind, at); err != nil {
		t.Fatalf("seed mfa factor %q: %v", factorID, err)
	}
}

func seedBootstrapCredentialRecoveryScopeLiveRecoveryCode(
	t *testing.T, ctx context.Context, db *sql.DB, userID, factorID, codeHash string, at time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_mfa_recovery_codes (user_id, factor_id, recovery_code_hash, status, created_at)
VALUES ($1, $2, $3, 'active', $4)`, userID, factorID, codeHash, at); err != nil {
		t.Fatalf("seed recovery code for factor %q: %v", factorID, err)
	}
}

type bootstrapCredentialRecoveryScopeLiveCodeState struct {
	status    string
	revokedAt sql.NullTime
}

func selectBootstrapCredentialRecoveryScopeLiveCodeState(
	t *testing.T, ctx context.Context, db *sql.DB, userID, codeHash string,
) bootstrapCredentialRecoveryScopeLiveCodeState {
	t.Helper()
	var state bootstrapCredentialRecoveryScopeLiveCodeState
	row := db.QueryRowContext(ctx, `
SELECT status, revoked_at
FROM identity_mfa_recovery_codes
WHERE user_id = $1 AND recovery_code_hash = $2`, userID, codeHash)
	if err := row.Scan(&state.status, &state.revokedAt); err != nil {
		t.Fatalf("select recovery code state for %q: %v", codeHash, err)
	}
	return state
}

func selectBootstrapCredentialRecoveryScopeLiveFactorStatus(
	t *testing.T, ctx context.Context, db *sql.DB, factorID string,
) string {
	t.Helper()
	var status string
	row := db.QueryRowContext(ctx, `SELECT status FROM identity_mfa_factors WHERE factor_id = $1`, factorID)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("select factor status for %q: %v", factorID, err)
	}
	return status
}
