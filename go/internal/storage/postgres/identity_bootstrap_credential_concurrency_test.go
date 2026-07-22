// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real-Postgres concurrency gate for epic #4962 / issue #4963.
//
// The fake-queryer unit tests in identity_bootstrap_credential_test.go prove
// the SQL text and Go control flow; they cannot exercise real unique-
// constraint contention, row locking, or advisory-lock serialization. This
// gate drives genuinely concurrent connections (one Postgres connection per
// goroutine, matching the reducer-contention gate's pattern) against one
// (tenant_id, workspace_id) row and proves:
//
//   - two racing Generate calls against an already-provisioned row both
//     report inserted=false (idempotent under real unique-constraint
//     contention, not just the fake's single-threaded ON CONFLICT path);
//   - a racing Consume and Reset on the same row never leave it in an
//     inconsistent state: consumed_at set with sealed_credential non-empty,
//     or consumed_at NULL with sealed_credential empty, are both forbidden —
//     the row is always exactly one of "retrievable" or "consumed";
//   - reset_count increases by exactly 1 for the single Reset call, proving
//     the advisory-locked read-modify-write is not double-applied under
//     concurrent load;
//   - the bcrypt hash in identity_local_credentials always matches whatever
//     Reset sealed, proving the envelope and the database password rotate
//     together and never diverge even under concurrent Consume pressure.
//
// Skipped unless a DSN is provided, matching the package's other
// real-Postgres proofs (see reducer_queue_contention_gate_test.go).
func TestBootstrapCredentialConcurrencyGateGenerateConsumeReset(t *testing.T) {
	dsn := bootstrapCredentialProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_BOOTSTRAP_CREDENTIAL_PROOF_DSN or ESHU_POSTGRES_DSN to run the bootstrap-credential concurrency gate")
	}

	ctx := context.Background()
	const rounds = 5
	for round := 0; round < rounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			ownerDB, schemaName := openBootstrapCredentialSchemaFixture(t, ctx, dsn)
			runBootstrapCredentialConcurrencyRound(t, ctx, dsn, schemaName, ownerDB, round)
		})
	}
}

func runBootstrapCredentialConcurrencyRound(
	t *testing.T,
	ctx context.Context,
	dsn string,
	schemaName string,
	ownerDB *sql.DB,
	round int,
) {
	t.Helper()

	tenantID := fmt.Sprintf("tenant-bc-%d", round)
	workspaceID := fmt.Sprintf("workspace-bc-%d", round)
	userID := fmt.Sprintf("user-bc-%d", round)
	subjectIDHash := fmt.Sprintf("sha256:subject-bc-%d", round)
	now := time.Now().UTC()

	seedBootstrapCredentialFixture(t, ctx, ownerDB, tenantID, workspaceID, userID, subjectIDHash, now)

	ownerStore := NewIdentitySubjectStore(SQLDB{DB: ownerDB})
	initialSeal := BootstrapCredentialSeal{
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		SubjectIDHash:    subjectIDHash,
		UsernameHash:     "sha256:username-bc",
		SealedCredential: "ESK1.key1.initial-nonce.initial-ciphertext",
		KeyID:            "key1",
		GeneratedAt:      now,
	}
	inserted, err := ownerStore.GenerateBootstrapCredential(ctx, initialSeal)
	if err != nil {
		t.Fatalf("seed GenerateBootstrapCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("seed GenerateBootstrapCredential() inserted = false, want true for a fresh row")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)

	// Two racing Generate calls against the now-existing row.
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := openBootstrapCredentialConn(t, ctx, dsn, schemaName)
			defer func() { _ = conn.Close() }()
			store := NewIdentitySubjectStore(SQLDB{DB: conn})
			racedInserted, err := store.GenerateBootstrapCredential(ctx, initialSeal)
			if err != nil {
				errs <- fmt.Errorf("racing generate[%d]: %w", i, err)
				return
			}
			if racedInserted {
				errs <- fmt.Errorf("racing generate[%d] inserted = true, want false for a pre-existing row", i)
			}
		}()
	}

	// One racing Consume.
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := openBootstrapCredentialConn(t, ctx, dsn, schemaName)
		defer func() { _ = conn.Close() }()
		store := NewIdentitySubjectStore(SQLDB{DB: conn})
		if _, err := store.ConsumeBootstrapCredential(ctx, tenantID, workspaceID, subjectIDHash, time.Now().UTC()); err != nil {
			errs <- fmt.Errorf("racing consume: %w", err)
		}
	}()

	// One racing Reset.
	resetPasswordHash := fmt.Sprintf("bcrypt:reset-hash-%d", round)
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := openBootstrapCredentialConn(t, ctx, dsn, schemaName)
		defer func() { _ = conn.Close() }()
		store := NewIdentitySubjectStore(SQLDB{DB: conn})
		err := store.ResetBootstrapCredential(ctx, ResetBootstrapCredentialInput{
			TenantID:               tenantID,
			WorkspaceID:            workspaceID,
			SealedCredential:       "ESK1.key2.reset-nonce.reset-ciphertext",
			KeyID:                  "key2",
			PasswordHash:           resetPasswordHash,
			PasswordAlgorithm:      "bcrypt",
			PasswordParametersHash: "sha256:bcrypt-cost",
			MFAFactorID:            fmt.Sprintf("id_reset-recovery-factor-%d", round),
			RecoveryCodeHash:       fmt.Sprintf("sha256:reset-recovery-code-%d", round),
			ResetAt:                time.Now().UTC(),
		})
		if err != nil {
			errs <- fmt.Errorf("racing reset: %w", err)
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	resetRecoveryCodeHash := fmt.Sprintf("sha256:reset-recovery-code-%d", round)
	assertBootstrapCredentialRowConsistent(t, ctx, ownerDB, tenantID, workspaceID, userID, resetPasswordHash, resetRecoveryCodeHash)
}

// assertBootstrapCredentialRowConsistent reads the final row state directly
// (bypassing the store, which only exposes the retrievable-row projection)
// and checks every invariant the concurrent round must preserve regardless of
// how Consume and Reset interleaved.
func assertBootstrapCredentialRowConsistent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	tenantID, workspaceID, userID, resetPasswordHash, resetRecoveryCodeHash string,
) {
	t.Helper()

	var (
		sealedCredential string
		consumedAt       sql.NullTime
		resetCount       int
	)
	row := db.QueryRowContext(ctx, `
SELECT sealed_credential, consumed_at, reset_count
FROM identity_bootstrap_credentials
WHERE tenant_id = $1 AND workspace_id = $2
`, tenantID, workspaceID)
	if err := row.Scan(&sealedCredential, &consumedAt, &resetCount); err != nil {
		t.Fatalf("read final bootstrap credential row: %v", err)
	}

	if resetCount != 1 {
		t.Fatalf("reset_count = %d, want exactly 1", resetCount)
	}

	// The row must be exactly one of "retrievable" (consumed_at NULL, ciphertext
	// present) or "consumed" (consumed_at set, ciphertext cleared) — never both
	// and never neither.
	retrievable := !consumedAt.Valid && sealedCredential != ""
	consumed := consumedAt.Valid && sealedCredential == ""
	if retrievable == consumed {
		t.Fatalf(
			"bootstrap credential row inconsistent: consumed_at.Valid=%t sealed_credential=%q (must be exactly one of retrievable/consumed)",
			consumedAt.Valid, sealedCredential,
		)
	}

	var passwordHash string
	credRow := db.QueryRowContext(ctx, `
SELECT password_hash FROM identity_local_credentials
WHERE user_id = $1 AND status = 'active' AND revoked_at IS NULL
`, userID)
	if err := credRow.Scan(&passwordHash); err != nil {
		t.Fatalf("read final local credential row: %v", err)
	}
	if passwordHash != resetPasswordHash {
		t.Fatalf("password_hash = %q, want %q (envelope and database password diverged under concurrency)", passwordHash, resetPasswordHash)
	}

	// The reset must also have re-enrolled exactly one active recovery-code
	// row carrying the NEW hash (issue #5602): concurrent Consume/Generate
	// pressure on the unrelated bootstrap-credential row must never leave the
	// MFA recovery factor stale, duplicated, or missing.
	var activeRecoveryCodeCount int
	row = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM identity_mfa_recovery_codes
WHERE user_id = $1 AND recovery_code_hash = $2 AND status = 'active' AND revoked_at IS NULL
`, userID, resetRecoveryCodeHash)
	if err := row.Scan(&activeRecoveryCodeCount); err != nil {
		t.Fatalf("read reset recovery code row: %v", err)
	}
	if activeRecoveryCodeCount != 1 {
		t.Fatalf("active recovery codes matching the reset hash = %d, want exactly 1", activeRecoveryCodeCount)
	}
}

func bootstrapCredentialProofDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_BOOTSTRAP_CREDENTIAL_PROOF_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// openBootstrapCredentialSchemaFixture creates an isolated throwaway schema and
// applies the minimal DDL chain the bootstrap-credential table's foreign keys
// need: ingestion_scopes (referenced by tenant_scope_grants inside the
// tenant_workspace_grants migration), tenant_workspace_grants (tenants,
// workspaces), identity_subjects (identity_users, identity_local_credentials,
// ...), and this issue's identity_bootstrap_credential migration.
func openBootstrapCredentialSchemaFixture(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("bootstrap_credential_%d", time.Now().UnixNano())
	db := openBootstrapCredentialSchemaConn(t, ctx, dsn, schemaName)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create bootstrap credential schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("tenant_workspace_grants"),
		MigrationSQL("identity_subjects"),
		MigrationSQL("identity_bootstrap_credential"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply bootstrap credential fixture schema: %v", err)
		}
	}
	return db, schemaName
}

// openBootstrapCredentialConn opens an independent single-connection handle
// bound to an already-created fixture schema, so concurrent goroutines each
// hold their own live Postgres connection and their statements truly
// interleave at the database rather than serializing behind one pooled
// connection (mirrors openReducerFairnessClaimerDB).
func openBootstrapCredentialConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	return openBootstrapCredentialSchemaConn(t, ctx, dsn, schemaName)
}

func openBootstrapCredentialSchemaConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}

func seedBootstrapCredentialFixture(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	tenantID, workspaceID, userID, subjectIDHash string,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'active', '', 'sha256:policy', $2, $2, NULL)
`, tenantID, now); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, $2, 'active', '', 'sha256:policy', $3, $3, NULL)
`, tenantID, workspaceID, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, profile_handle_hash, created_at, updated_at, disabled_at, tombstoned_at)
VALUES ($1, $2, 'active', '', $3, $3, NULL, NULL)
`, userID, subjectIDHash, now); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_local_credentials (credential_id, user_id, password_hash, password_algorithm, password_parameters_hash, status, created_at, rotated_at, expires_at, revoked_at)
VALUES ($1, $2, 'bcrypt:initial-hash', 'bcrypt', 'sha256:bcrypt-cost', 'active', $3, $3, NULL, NULL)
`, userID+"-cred", userID, now); err != nil {
		t.Fatalf("seed local credential: %v", err)
	}
}
