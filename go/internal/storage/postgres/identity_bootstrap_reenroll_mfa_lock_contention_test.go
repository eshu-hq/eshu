// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// Real-Postgres concurrency gate proving the two remaining 3456-guarded
// factor-mutating paths now also take the per-user MFA-reset advisory lock, so
// the duplicate-active-factor hazard lockLocalIdentityMFAReset closes for
// ResetLocalIdentityMFA cannot be re-opened through a sibling writer.
//
// Background: lockLocalIdentityMFAReset (identity_local_mfa_reset_lock.go)
// serializes ResetLocalIdentityMFA per user because identity_mfa_factors has
// no unique "one active factor per (user_id, factor_kind)" constraint, so two
// concurrent revoke/insert sequences can each observe zero active rows and
// both insert one — leaving two simultaneously active recovery-code factors.
// ResetLocalIdentityMFA takes ONLY the per-user key; ResetBootstrapCredential
// and CompleteSetupMFA take ONLY pg_advisory_xact_lock(3456). The lock sets
// were disjoint, so ResetBootstrapCredential-vs-ResetLocalIdentityMFA and
// CompleteSetupMFA-vs-ResetLocalIdentityMFA for the same user shared no lock
// and could interleave into the exact duplicate-active-factor state
// TestLocalIdentityMFAResetRaceWithoutLockDuplicatesActiveFactor reproduces.
//
// Both fixes acquire the per-user key AFTER 3456 (never before), preserving
// the 3455 -> 3456 -> per-user linear lock hierarchy the lock helper's doc
// comment documents, so no wait-for cycle can form.
//
// Each test is the blocking half of the RED->GREEN proof: connection 1 holds
// the per-user advisory lock open, and the full production method on
// connection 2 must park on that same lock (proven by both a timing assertion
// and a pg_locks ungranted-advisory assertion) until connection 1 releases.
// Without the per-user lock in the production method, connection 2 has no
// reason to wait and returns immediately, failing the block assertion — that
// is the RED state on unmodified main.

// TestResetBootstrapCredentialBlocksOnConcurrentMFAResetLock proves
// ResetBootstrapCredential now serializes against a held per-user MFA-reset
// lock for the same user, so it can no longer race ResetLocalIdentityMFA into
// duplicate active recovery-code factors.
func TestResetBootstrapCredentialBlocksOnConcurrentMFAResetLock(t *testing.T) {
	dsn := bootstrapCredentialProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_BOOTSTRAP_CREDENTIAL_PROOF_DSN or ESHU_POSTGRES_DSN to run the bootstrap re-enroll MFA-lock contention gate")
	}

	ctx := context.Background()
	ownerDB, schemaName := openBootstrapCredentialSchemaFixture(t, ctx, dsn)

	tenantID := "tenant-reenroll-lock"
	workspaceID := "workspace-reenroll-lock"
	userID := "user-reenroll-lock"
	subjectIDHash := "sha256:subject-reenroll-lock"
	now := time.Now().UTC()
	seedBootstrapCredentialFixture(t, ctx, ownerDB, tenantID, workspaceID, userID, subjectIDHash, now)

	ownerStore := NewIdentitySubjectStore(SQLDB{DB: ownerDB})
	inserted, err := ownerStore.GenerateBootstrapCredential(ctx, BootstrapCredentialSeal{
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		SubjectIDHash:    subjectIDHash,
		UsernameHash:     "sha256:username-reenroll",
		SealedCredential: "ESK1.key1.nonce.ciphertext",
		KeyID:            "key1",
		GeneratedAt:      now,
	})
	if err != nil {
		t.Fatalf("seed GenerateBootstrapCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("seed GenerateBootstrapCredential() inserted = false, want true for a fresh row")
	}

	releaseHolder := holdPerUserMFAResetLock(t, ctx, dsn, schemaName, userID)
	defer releaseHolder()

	waiterConn := openBootstrapCredentialSchemaConn(t, ctx, dsn, schemaName)
	waiterPID := backendPID(t, ctx, waiterConn)
	waiterStore := NewIdentitySubjectStore(SQLDB{DB: waiterConn})

	done := make(chan error, 1)
	go func() {
		done <- waiterStore.ResetBootstrapCredential(ctx, ResetBootstrapCredentialInput{
			TenantID:               tenantID,
			WorkspaceID:            workspaceID,
			SealedCredential:       "ESK1.key2.reset-nonce.reset-ciphertext",
			KeyID:                  "key2",
			PasswordHash:           "bcrypt:reset-hash",
			PasswordAlgorithm:      "bcrypt",
			PasswordParametersHash: "sha256:bcrypt-cost",
			MFAFactorID:            "id_reset-recovery-factor",
			RecoveryCodeHash:       "sha256:reset-recovery-code",
			ResetAt:                time.Now().UTC(),
		})
	}()

	assertBlockedThenReleased(t, ctx, ownerDB, waiterPID, done, releaseHolder, "ResetBootstrapCredential")
	_ = waiterConn.Close()
}

// TestCompleteSetupMFABlocksOnConcurrentMFAResetLock proves CompleteSetupMFA
// now serializes against a held per-user MFA-reset lock for the same user, so
// the wizard MFA-setup completion path can no longer race ResetLocalIdentityMFA
// into duplicate active recovery-code factors.
func TestCompleteSetupMFABlocksOnConcurrentMFAResetLock(t *testing.T) {
	dsn := bootstrapCredentialProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_BOOTSTRAP_CREDENTIAL_PROOF_DSN or ESHU_POSTGRES_DSN to run the setup-completion MFA-lock contention gate")
	}

	ctx := context.Background()
	ownerDB, schemaName := openBootstrapCredentialSchemaFixture(t, ctx, dsn)

	tenantID := "tenant-setup-lock"
	workspaceID := "workspace-setup-lock"
	userID := "user-setup-lock"
	subjectIDHash := "sha256:subject-setup-lock"
	now := time.Now().UTC()
	seedBootstrapCredentialFixture(t, ctx, ownerDB, tenantID, workspaceID, userID, subjectIDHash, now)

	ownerStore := NewIdentitySubjectStore(SQLDB{DB: ownerDB})
	inserted, err := ownerStore.GenerateBootstrapCredential(ctx, BootstrapCredentialSeal{
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		SubjectIDHash:    subjectIDHash,
		UsernameHash:     "sha256:username-setup",
		SealedCredential: "ESK1.key1.nonce.ciphertext",
		KeyID:            "key1",
		GeneratedAt:      now,
	})
	if err != nil {
		t.Fatalf("seed GenerateBootstrapCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("seed GenerateBootstrapCredential() inserted = false, want true for a fresh row")
	}

	releaseHolder := holdPerUserMFAResetLock(t, ctx, dsn, schemaName, userID)
	defer releaseHolder()

	waiterConn := openBootstrapCredentialSchemaConn(t, ctx, dsn, schemaName)
	waiterPID := backendPID(t, ctx, waiterConn)
	waiterStore := NewIdentitySubjectStore(SQLDB{DB: waiterConn})

	done := make(chan error, 1)
	go func() {
		_, err := waiterStore.CompleteSetupMFA(ctx, CompleteSetupMFAInput{
			TenantID:      tenantID,
			WorkspaceID:   workspaceID,
			SubjectIDHash: subjectIDHash,
			MFA: LocalIdentityMFAReset{
				UserID:             userID,
				MFAFactorID:        "mfa-factor-setup",
				MFAFactorKind:      "recovery_code",
				RecoveryCodeHashes: []string{"sha256:setup-code"},
				ResetAt:            time.Now().UTC(),
			},
		})
		done <- err
	}()

	assertBlockedThenReleased(t, ctx, ownerDB, waiterPID, done, releaseHolder, "CompleteSetupMFA")
	_ = waiterConn.Close()
}

// holdPerUserMFAResetLock opens a dedicated connection and transaction that
// acquires and holds the per-user MFA-reset advisory lock for userID open
// (uncommitted), standing in for a first ResetLocalIdentityMFA that has
// reached lockLocalIdentityMFAReset but not yet committed. It returns an
// idempotent release func that rolls the holder transaction back.
func holdPerUserMFAResetLock(t *testing.T, ctx context.Context, dsn, schemaName, userID string) func() {
	t.Helper()
	holderConn := openBootstrapCredentialSchemaConn(t, ctx, dsn, schemaName)
	holderTx, err := SQLDB{DB: holderConn}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		if err := holderTx.Rollback(); err != nil {
			t.Fatalf("rollback holder tx: %v", err)
		}
	}
	if err := lockLocalIdentityMFAReset(ctx, holderTx, userID); err != nil {
		release()
		t.Fatalf("holder acquire per-user MFA reset lock: %v", err)
	}
	return release
}

// backendPID reads the Postgres backend pid serving conn. Because the fixture
// connections cap MaxOpenConns at 1, this pid is stable for every subsequent
// statement on conn, including the one the waiter parks on.
func backendPID(t *testing.T, ctx context.Context, conn *sql.DB) int {
	t.Helper()
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}
	return pid
}

// assertBlockedThenReleased proves the waiter goroutine is parked on the
// per-user advisory lock (timing + pg_locks corroboration) while the holder
// holds it, then that it completes without error once the holder releases.
func assertBlockedThenReleased(
	t *testing.T,
	ctx context.Context,
	ownerDB *sql.DB,
	waiterPID int,
	done <-chan error,
	releaseHolder func(),
	label string,
) {
	t.Helper()

	// Deterministic proof #1: the waiter has not completed while the holder
	// still holds the per-user advisory lock. On unmodified main (no per-user
	// lock in the production method) the waiter returns almost immediately and
	// fails this select — the RED state this gate turns GREEN.
	select {
	case err := <-done:
		t.Fatalf("%s returned (err=%v) while the holder still held the per-user advisory lock; want it blocked", label, err)
	case <-time.After(1500 * time.Millisecond):
		// Still blocked, as required.
	}

	// Deterministic proof #2: pg_locks corroborates the waiter's own backend
	// is parked on an ungranted advisory lock, not merely slow.
	assertBackendWaitingOnAdvisoryLock(t, ctx, ownerDB, waiterPID)

	releaseHolder()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s error = %v after the holder released the per-user advisory lock", label, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not complete within 5s after the holder released the per-user advisory lock", label)
	}
}
