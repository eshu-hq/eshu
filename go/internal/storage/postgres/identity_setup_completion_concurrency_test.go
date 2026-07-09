// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Real-Postgres concurrency gate for #4990 (issue #4965 code review).
//
// The fake-queryer unit tests in identity_setup_completion_test.go prove the
// SQL text and Go control flow; they cannot exercise real advisory-lock
// serialization or unique-constraint contention. This gate drives genuinely
// concurrent connections (one Postgres connection per goroutine, mirroring
// identity_bootstrap_credential_concurrency_test.go) against one
// (tenant_id, workspace_id, subject_id_hash) row and proves:
//
//   - exactly one of N concurrent CompleteSetupMFA calls reports
//     completed=true; the rest report completed=false with no error;
//   - the losing callers never mutate identity_mfa_factors or
//     identity_mfa_recovery_codes — the final active-factor count is exactly
//     1, not orphaned rows from a caller who lost the race;
//   - the bootstrap credential row is left in the "consumed" state
//     (consumed_at set, sealed_credential cleared) — never "retrievable"
//     and never inconsistent.
//
// Skipped unless a DSN is provided, matching the package's other
// real-Postgres proofs.
func TestCompleteSetupMFAConcurrencyGateExactlyOneCompletes(t *testing.T) {
	dsn := bootstrapCredentialProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_BOOTSTRAP_CREDENTIAL_PROOF_DSN or ESHU_POSTGRES_DSN to run the setup-mfa-completion concurrency gate")
	}

	ctx := context.Background()
	const rounds = 5
	for round := 0; round < rounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			ownerDB, schemaName := openBootstrapCredentialSchemaFixture(t, ctx, dsn)
			runCompleteSetupMFAConcurrencyRound(t, ctx, dsn, schemaName, ownerDB, round)
		})
	}
}

func runCompleteSetupMFAConcurrencyRound(
	t *testing.T,
	ctx context.Context,
	dsn string,
	schemaName string,
	ownerDB *sql.DB,
	round int,
) {
	t.Helper()

	tenantID := fmt.Sprintf("tenant-mfa-%d", round)
	workspaceID := fmt.Sprintf("workspace-mfa-%d", round)
	userID := fmt.Sprintf("user-mfa-%d", round)
	subjectIDHash := fmt.Sprintf("sha256:subject-mfa-%d", round)
	now := time.Now().UTC()

	seedBootstrapCredentialFixture(t, ctx, ownerDB, tenantID, workspaceID, userID, subjectIDHash, now)

	ownerStore := NewIdentitySubjectStore(SQLDB{DB: ownerDB})
	seal := BootstrapCredentialSeal{
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		SubjectIDHash:    subjectIDHash,
		UsernameHash:     "sha256:username-mfa",
		SealedCredential: "ESK1.key1.nonce.ciphertext",
		KeyID:            "key1",
		GeneratedAt:      now,
	}
	inserted, err := ownerStore.GenerateBootstrapCredential(ctx, seal)
	if err != nil {
		t.Fatalf("seed GenerateBootstrapCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("seed GenerateBootstrapCredential() inserted = false, want true for a fresh row")
	}

	const racers = 5
	var wg sync.WaitGroup
	results := make(chan bool, racers)
	errs := make(chan error, racers)
	for i := 0; i < racers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := openBootstrapCredentialConn(t, ctx, dsn, schemaName)
			defer func() { _ = conn.Close() }()
			store := NewIdentitySubjectStore(SQLDB{DB: conn})
			completed, err := store.CompleteSetupMFA(ctx, CompleteSetupMFAInput{
				TenantID:      tenantID,
				WorkspaceID:   workspaceID,
				SubjectIDHash: subjectIDHash,
				MFA: LocalIdentityMFAReset{
					UserID:             userID,
					MFAFactorID:        fmt.Sprintf("mfa-factor-%d-%d", round, i),
					MFAFactorKind:      "recovery_code",
					RecoveryCodeHashes: []string{fmt.Sprintf("sha256:code-%d-%d", round, i)},
					ResetAt:            time.Now().UTC(),
				},
			})
			if err != nil {
				errs <- fmt.Errorf("racer[%d]: %w", i, err)
				return
			}
			results <- completed
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	completedCount := 0
	for c := range results {
		if c {
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("completedCount = %d across %d racers, want exactly 1 (exactly one completion must win)", completedCount, racers)
	}

	assertCompleteSetupMFARowConsistent(t, ctx, ownerDB, tenantID, workspaceID, userID)
}

// assertCompleteSetupMFARowConsistent reads the final row state directly and
// checks every invariant the concurrent round must preserve regardless of
// how the racing completions interleaved: exactly one active MFA factor (no
// losing racer's factor ever got written), and the bootstrap credential
// consumed (never left retrievable, never inconsistent).
func assertCompleteSetupMFARowConsistent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	tenantID, workspaceID, userID string,
) {
	t.Helper()

	var activeFactorCount int
	factorRow := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM identity_mfa_factors
WHERE user_id = $1 AND status = 'active' AND revoked_at IS NULL
`, userID)
	if err := factorRow.Scan(&activeFactorCount); err != nil {
		t.Fatalf("read final mfa factor count: %v", err)
	}
	if activeFactorCount != 1 {
		t.Fatalf("active mfa factor count = %d, want exactly 1 (a losing racer must never persist a factor)", activeFactorCount)
	}

	var sealedCredential string
	var consumedAt sql.NullTime
	credRow := db.QueryRowContext(ctx, `
SELECT sealed_credential, consumed_at
FROM identity_bootstrap_credentials
WHERE tenant_id = $1 AND workspace_id = $2
`, tenantID, workspaceID)
	if err := credRow.Scan(&sealedCredential, &consumedAt); err != nil {
		t.Fatalf("read final bootstrap credential row: %v", err)
	}
	if !consumedAt.Valid || sealedCredential != "" {
		t.Fatalf(
			"bootstrap credential not cleanly consumed after exactly one completion: consumed_at.Valid=%t sealed_credential=%q",
			consumedAt.Valid, sealedCredential,
		)
	}
}
