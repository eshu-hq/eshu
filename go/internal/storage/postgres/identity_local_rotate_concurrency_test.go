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

// Real-Postgres concurrency gate for issue #4976.
//
// The fake-queryer unit tests in identity_local_rotate_test.go prove the SQL
// text and Go control flow (the row-locked select includes "FOR UPDATE OF
// c"); they cannot exercise genuine row-lock contention or Read Committed's
// EvalPlanQual recheck. This gate drives two genuinely concurrent Postgres
// connections rotating the SAME credential with the SAME (still-valid at
// start) current password, and proves:
//
//   - exactly one of the two concurrent RotateLocalIdentityPassword calls
//     reports Authenticated=true;
//   - the other reports LocalIdentityAuthInvalid, because its row-locked read
//     blocks on the first rotation's row lock and, once unblocked, re-checks
//     "c.status = 'active'" against the first rotation's committed result and
//     finds the credential already revoked -- never a stale-password double
//     accept;
//   - exactly one active identity_local_credentials row survives, and its
//     password_hash is the winning goroutine's new hash.
//
// Skipped unless a DSN is provided, matching the package's other real-
// Postgres proofs (see identity_bootstrap_credential_concurrency_test.go).
func TestRotateLocalIdentityPasswordConcurrencyGateExactlyOneWinner(t *testing.T) {
	dsn := rotationConcurrencyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_ROTATION_CONCURRENCY_PROOF_DSN or ESHU_POSTGRES_DSN to run the password-rotation concurrency gate")
	}

	ctx := context.Background()
	const rounds = 5
	for round := 0; round < rounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			ownerDB, schemaName := openRotationConcurrencySchemaFixture(t, ctx, dsn)
			runRotationConcurrencyRound(t, ctx, dsn, schemaName, ownerDB, round)
		})
	}
}

func runRotationConcurrencyRound(
	t *testing.T,
	ctx context.Context,
	dsn string,
	schemaName string,
	ownerDB *sql.DB,
	round int,
) {
	t.Helper()

	userID := fmt.Sprintf("user-rc-%d", round)
	subjectIDHash := fmt.Sprintf("sha256:subject-rc-%d", round)
	originalPassword := fmt.Sprintf("original-password-%d", round)
	now := time.Now().UTC()

	seedRotationConcurrencyFixture(t, ctx, ownerDB, userID, subjectIDHash, originalPassword, now)

	type outcome struct {
		result LocalIdentityAuthenticationResult
		err    error
	}
	results := make([]outcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := openRotationConcurrencyConn(t, ctx, dsn, schemaName)
			defer func() { _ = conn.Close() }()
			store := NewIdentitySubjectStore(SQLDB{DB: conn})
			result, err := store.RotateLocalIdentityPassword(ctx, LocalIdentityPasswordRotation{
				SubjectIDHash:             subjectIDHash,
				CurrentPassword:           originalPassword,
				NewPasswordHash:           fmt.Sprintf("bcrypt:rotated-hash-%d-%d", round, i),
				NewPasswordAlgorithm:      "bcrypt",
				NewPasswordParametersHash: "sha256:bcrypt-cost",
				CredentialID:              fmt.Sprintf("%s-cred-rotated-%d", userID, i),
				Now:                       time.Now().UTC(),
			})
			results[i] = outcome{result: result, err: err}
		}()
	}
	wg.Wait()

	winners := 0
	invalids := 0
	for i, out := range results {
		if out.err != nil {
			t.Fatalf("goroutine[%d] RotateLocalIdentityPassword() error = %v, want nil", i, out.err)
		}
		switch {
		case out.result.Authenticated && out.result.Status == LocalIdentityAuthAuthenticated:
			winners++
		case !out.result.Authenticated && out.result.Status == LocalIdentityAuthInvalid:
			invalids++
		default:
			t.Fatalf("goroutine[%d] result = %#v, want either authenticated or invalid", i, out.result)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1 (round %d)", winners, round)
	}
	if invalids != 1 {
		t.Fatalf("invalids = %d, want exactly 1 (round %d)", invalids, round)
	}

	assertRotationConcurrencyRowConsistent(t, ctx, ownerDB, userID)
}

// assertRotationConcurrencyRowConsistent reads the final credential state
// directly and proves exactly one active row survives regardless of goroutine
// interleaving, and that row's must_change_password is false.
func assertRotationConcurrencyRowConsistent(t *testing.T, ctx context.Context, db *sql.DB, userID string) {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT password_hash, must_change_password
FROM identity_local_credentials
WHERE user_id = $1 AND status = 'active' AND revoked_at IS NULL
`, userID)
	if err != nil {
		t.Fatalf("read final credential rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var passwordHashes []string
	for rows.Next() {
		var passwordHash string
		var mustChangePassword bool
		if err := rows.Scan(&passwordHash, &mustChangePassword); err != nil {
			t.Fatalf("scan final credential row: %v", err)
		}
		if mustChangePassword {
			t.Fatalf("surviving active credential has must_change_password=true, want false (rotation always clears it)")
		}
		passwordHashes = append(passwordHashes, passwordHash)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate final credential rows: %v", err)
	}
	if len(passwordHashes) != 1 {
		t.Fatalf("active credential rows = %d, want exactly 1 (got %#v)", len(passwordHashes), passwordHashes)
	}
	if !strings.HasPrefix(passwordHashes[0], "bcrypt:rotated-hash-") {
		t.Fatalf("surviving credential password_hash = %q, want the winning goroutine's rotated hash", passwordHashes[0])
	}
}

func rotationConcurrencyProofDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_ROTATION_CONCURRENCY_PROOF_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// openRotationConcurrencySchemaFixture creates an isolated throwaway schema
// and applies the minimal DDL chain identity_local_credentials needs:
// ingestion_scopes, tenant_workspace_grants (tenants, workspaces),
// identity_subjects (identity_users, identity_local_credentials, ...), and
// this issue's must_change_password column migration.
func openRotationConcurrencySchemaFixture(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("rotation_concurrency_%d", time.Now().UnixNano())
	db := openRotationConcurrencySchemaConn(t, ctx, dsn, schemaName)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create rotation concurrency schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("tenant_workspace_grants"),
		MigrationSQL("identity_subjects"),
		MigrationSQL("identity_local_credentials_must_change_password"),
		MigrationSQL("identity_bootstrap_credential"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply rotation concurrency fixture schema: %v", err)
		}
	}
	return db, schemaName
}

// openRotationConcurrencyConn opens an independent single-connection handle
// bound to an already-created fixture schema, so concurrent goroutines each
// hold their own live Postgres connection and their statements truly
// interleave at the database rather than serializing behind one pooled
// connection (mirrors openBootstrapCredentialConn).
func openRotationConcurrencyConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	return openRotationConcurrencySchemaConn(t, ctx, dsn, schemaName)
}

func openRotationConcurrencySchemaConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
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

func seedRotationConcurrencyFixture(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	userID, subjectIDHash, password string,
	now time.Time,
) {
	t.Helper()
	tenantID := "tenant-rc"
	workspaceID := "workspace-rc"
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, 'active', '', 'sha256:policy', $2, $2, NULL)
ON CONFLICT (tenant_id) DO NOTHING
`, tenantID, now); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (tenant_id, workspace_id, status, display_handle_hash, policy_revision_hash, created_at, updated_at, tombstoned_at)
VALUES ($1, $2, 'active', '', 'sha256:policy', $3, $3, NULL)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING
`, tenantID, workspaceID, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, profile_handle_hash, created_at, updated_at, disabled_at, tombstoned_at)
VALUES ($1, $2, 'active', '', $3, $3, NULL, NULL)
`, userID, subjectIDHash, now); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
	passwordHash := mustBcryptHash(t, password)
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_local_credentials (credential_id, user_id, password_hash, password_algorithm, password_parameters_hash, status, created_at, rotated_at, expires_at, revoked_at, must_change_password)
VALUES ($1, $2, $3, 'bcrypt', 'sha256:bcrypt-cost', 'active', $4, $4, NULL, NULL, true)
`, userID+"-cred", userID, passwordHash, now); err != nil {
		t.Fatalf("seed local credential: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_tenant_memberships (tenant_id, workspace_id, user_id, status, membership_source, policy_revision_hash, effective_at, expires_at, disabled_at, tombstoned_at, created_at, updated_at)
VALUES ($1, $2, $3, 'active', 'bootstrap', 'sha256:policy', $4, NULL, NULL, NULL, $4, $4)
`, tenantID, workspaceID, userID, now); err != nil {
		t.Fatalf("seed tenant membership: %v", err)
	}
}
