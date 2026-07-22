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

// Real-Postgres concurrency gate for the local-identity MFA reset race: two
// concurrent ResetLocalIdentityMFA calls for the SAME user, with no advisory
// lock and no unique constraint on (user_id, factor_kind) for active
// identity_mfa_factors rows (identity_mfa_factors_user_active_idx is a
// non-unique partial index — see identity_subjects.go), can both commit an
// active recovery-code factor for that user. AuthenticateLocalIdentity still
// works (it looks up by (user_id, recovery_code_hash)), but the duplicate
// active state is silent and must be prevented by construction.
//
// The fake-queryer unit tests in identity_local_lifecycle_test.go-adjacent
// files prove SQL text and control flow; they cannot exercise real
// UPDATE/INSERT interleaving across two live connections. This gate mirrors
// identity_bootstrap_credential_concurrency_test.go's pattern: one Postgres
// connection per goroutine against an isolated throwaway schema.
//
// Skipped unless a DSN is provided, matching the package's other real-
// Postgres proofs.
func TestLocalIdentityMFAResetConcurrencyGateSingleActiveFactor(t *testing.T) {
	dsn := localIdentityMFAResetProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_LOCAL_IDENTITY_MFA_RESET_PROOF_DSN or ESHU_POSTGRES_DSN to run the local-identity MFA reset concurrency gate")
	}

	ctx := context.Background()
	const rounds = 5
	for round := 0; round < rounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			ownerDB, schemaName := openLocalIdentityMFAResetSchemaFixture(t, ctx, dsn)
			runLocalIdentityMFAResetConcurrencyRound(t, ctx, dsn, schemaName, ownerDB, round)
		})
	}
}

func runLocalIdentityMFAResetConcurrencyRound(
	t *testing.T,
	ctx context.Context,
	dsn string,
	schemaName string,
	ownerDB *sql.DB,
	round int,
) {
	t.Helper()

	userID := fmt.Sprintf("user-mfa-reset-%d", round)
	subjectIDHash := fmt.Sprintf("sha256:subject-mfa-reset-%d", round)
	now := time.Now().UTC()

	seedLocalIdentityMFAResetFixtureUser(t, ctx, ownerDB, userID, subjectIDHash, now)

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := openLocalIdentityMFAResetConn(t, ctx, dsn, schemaName)
			defer func() { _ = conn.Close() }()
			store := NewIdentitySubjectStore(SQLDB{DB: conn})
			err := store.ResetLocalIdentityMFA(ctx, LocalIdentityMFAReset{
				UserID:              userID,
				MFAFactorID:         fmt.Sprintf("factor-mfa-reset-%d-racer-%d", round, i),
				MFAFactorKind:       "recovery_code",
				MFACredentialHandle: "",
				RecoveryCodeHashes: []string{
					fmt.Sprintf("sha256:recovery-%d-racer-%d-a", round, i),
					fmt.Sprintf("sha256:recovery-%d-racer-%d-b", round, i),
				},
				ResetAt: time.Now().UTC(),
			})
			if err != nil {
				errs <- fmt.Errorf("racing reset[%d]: %w", i, err)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	assertLocalIdentityMFAResetSingleActiveFactor(t, ctx, ownerDB, userID)
}

// assertLocalIdentityMFAResetSingleActiveFactor reads the final row state
// directly and proves the invariant the concurrent round must preserve
// regardless of how the two resets interleaved: at most one active,
// unrevoked identity_mfa_factors row for the user.
func assertLocalIdentityMFAResetSingleActiveFactor(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	userID string,
) {
	t.Helper()

	var activeFactors int
	row := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM identity_mfa_factors
WHERE user_id = $1 AND status = 'active' AND revoked_at IS NULL
`, userID)
	if err := row.Scan(&activeFactors); err != nil {
		t.Fatalf("read active mfa factor count: %v", err)
	}
	if activeFactors != 1 {
		t.Fatalf("active identity_mfa_factors rows for user = %d, want exactly 1 (duplicate active MFA state)", activeFactors)
	}
}

func localIdentityMFAResetProofDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_LOCAL_IDENTITY_MFA_RESET_PROOF_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// openLocalIdentityMFAResetSchemaFixture creates an isolated throwaway schema
// and applies the identity_subjects migration, which defines identity_users,
// identity_mfa_factors, and identity_mfa_recovery_codes with no foreign-key
// dependency on tenant/workspace tables.
func openLocalIdentityMFAResetSchemaFixture(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("local_identity_mfa_reset_%d", time.Now().UnixNano())
	db := openLocalIdentityMFAResetSchemaConn(t, ctx, dsn, schemaName)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create local identity mfa reset schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("tenant_workspace_grants"),
		MigrationSQL("identity_subjects"),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply local identity mfa reset fixture schema: %v", err)
		}
	}
	return db, schemaName
}

// openLocalIdentityMFAResetConn opens an independent single-connection handle
// bound to an already-created fixture schema, so concurrent goroutines each
// hold their own live Postgres connection and their statements truly
// interleave at the database rather than serializing behind one pooled
// connection.
func openLocalIdentityMFAResetConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	return openLocalIdentityMFAResetSchemaConn(t, ctx, dsn, schemaName)
}

func openLocalIdentityMFAResetSchemaConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
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

func seedLocalIdentityMFAResetFixtureUser(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	userID, subjectIDHash string,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, profile_handle_hash, created_at, updated_at, disabled_at, tombstoned_at)
VALUES ($1, $2, 'active', '', $3, $3, NULL, NULL)
`, userID, subjectIDHash, now); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
}
