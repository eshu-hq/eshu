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

// Deterministic real-Postgres proofs for the local-identity MFA reset
// advisory lock, complementing
// TestLocalIdentityMFAResetConcurrencyGateSingleActiveFactor in
// identity_local_mfa_reset_concurrency_test.go.
//
// That gate launches two full ResetLocalIdentityMFA calls and asserts
// exactly one active factor survives, but nothing in it forces the
// hazardous interleaving: the first reset can commit in full before the
// second one even starts, so the assertion passes even with
// lockLocalIdentityMFAReset removed entirely — a false green (PR #5624
// review). The two tests below replace that hope-based coverage with two
// deterministic proofs:
//
//   - TestLocalIdentityMFAResetLockBlocksConcurrentResetForSameUser holds
//     the per-user advisory lock open on one connection and proves a
//     second ResetLocalIdentityMFA call for the SAME user genuinely blocks
//     — not "usually finishes second" — until the first transaction
//     releases the lock. This is the primary regression gate: it fails
//     immediately if lockLocalIdentityMFAReset is ever removed or
//     narrowed, because the second call would then have nothing to block
//     on.
//   - TestLocalIdentityMFAResetRaceWithoutLockDuplicatesActiveFactor
//     reproduces ResetLocalIdentityMFA's exact revoke/insert statement
//     sequence on two connections with the advisory lock call skipped and
//     a barrier placed after both connections have executed their revoke
//     statements (so both deterministically observe zero active factors)
//     and before either inserts its replacement. Without serialization
//     this always lands two simultaneously active identity_mfa_factors
//     rows, proving the hazard the lock exists to prevent is real, not
//     theoretical. It calls the same package-private query constants and
//     insertLocalIdentityMFA helper ResetLocalIdentityMFA calls — not a
//     reimplementation — with only the lock call omitted and the barrier
//     added, so it stays symmetric with production behavior aside from
//     the two things deliberately under test.
//
// A barrier placed AFTER both transactions' revoke statements but INSIDE
// ResetLocalIdentityMFA itself would deadlock when the lock is present:
// the second transaction blocks inside lockLocalIdentityMFAReset, before
// it ever reaches its revoke statement, and so never reaches the barrier
// while the first transaction waits at the barrier for it. That is why the
// race reproduction below drives the statement sequence directly with the
// lock call omitted, instead of adding a barrier hook inside
// ResetLocalIdentityMFA.

// TestLocalIdentityMFAResetLockBlocksConcurrentResetForSameUser is the
// primary regression gate for lockLocalIdentityMFAReset. It proves the
// per-user advisory lock genuinely serializes two ResetLocalIdentityMFA
// calls for the same user: a second call blocks for as long as the first
// transaction holds the lock open, and proceeds only after that
// transaction releases it.
func TestLocalIdentityMFAResetLockBlocksConcurrentResetForSameUser(t *testing.T) {
	dsn := localIdentityMFAResetProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_LOCAL_IDENTITY_MFA_RESET_PROOF_DSN or ESHU_POSTGRES_DSN to run the local-identity MFA reset lock-contention gate")
	}

	ctx := context.Background()
	ownerDB, schemaName := openLocalIdentityMFAResetSchemaFixture(t, ctx, dsn)

	userID := "user-mfa-lock-contention"
	subjectIDHash := "sha256:subject-mfa-lock-contention"
	now := time.Now().UTC()
	seedLocalIdentityMFAResetFixtureUser(t, ctx, ownerDB, userID, subjectIDHash, now)

	// Connection 1 acquires and holds the per-user advisory lock inside an
	// open (uncommitted) transaction, standing in for a first
	// ResetLocalIdentityMFA call that has reached lockLocalIdentityMFAReset
	// but not yet committed.
	holderConn := openLocalIdentityMFAResetConn(t, ctx, dsn, schemaName)
	holderTx, err := SQLDB{DB: holderConn}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	holderReleased := false
	releaseHolder := func() {
		if holderReleased {
			return
		}
		holderReleased = true
		if err := holderTx.Rollback(); err != nil {
			t.Fatalf("rollback holder tx: %v", err)
		}
	}
	defer releaseHolder()
	if err := lockLocalIdentityMFAReset(ctx, holderTx, userID); err != nil {
		t.Fatalf("holder acquire lock: %v", err)
	}

	waiterConn := openLocalIdentityMFAResetConn(t, ctx, dsn, schemaName)
	var waiterPID int
	if err := waiterConn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&waiterPID); err != nil {
		t.Fatalf("read waiter backend pid: %v", err)
	}
	waiterStore := NewIdentitySubjectStore(SQLDB{DB: waiterConn})

	done := make(chan error, 1)
	go func() {
		done <- waiterStore.ResetLocalIdentityMFA(ctx, LocalIdentityMFAReset{
			UserID:              userID,
			MFAFactorID:         "factor-mfa-lock-contention-waiter",
			MFAFactorKind:       "recovery_code",
			MFACredentialHandle: "",
			RecoveryCodeHashes: []string{
				"sha256:recovery-lock-contention-waiter-a",
				"sha256:recovery-lock-contention-waiter-b",
			},
			ResetAt: time.Now().UTC(),
		})
	}()

	// Deterministic proof #1: the waiter has not completed while connection
	// 1 still holds the per-user advisory lock open. Without the lock in
	// ResetLocalIdentityMFA, the waiter has no reason to wait on the
	// holder's open transaction and would return almost immediately,
	// failing this select — this is exactly what happens if
	// lockLocalIdentityMFAReset is removed (proven below under "RED ->
	// GREEN" in the PR description).
	select {
	case err := <-done:
		t.Fatalf("waiter ResetLocalIdentityMFA returned (err=%v) while the holder still held the per-user advisory lock; want it blocked", err)
	case <-time.After(1500 * time.Millisecond):
		// Still blocked, as required.
	}

	// Deterministic proof #2: pg_locks corroborates the waiter's own
	// backend is parked on an ungranted advisory lock, not merely slow for
	// an unrelated reason.
	assertBackendWaitingOnAdvisoryLock(t, ctx, ownerDB, waiterPID)

	// Release the holder's lock and prove the waiter now proceeds.
	releaseHolder()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter ResetLocalIdentityMFA() error = %v after the holder released the lock", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter ResetLocalIdentityMFA did not complete within 5s after the holder released the lock")
	}

	_ = waiterConn.Close()
}

// assertBackendWaitingOnAdvisoryLock proves the given backend pid is parked
// waiting to acquire an advisory lock it does not yet hold, corroborating
// the timing-based block assertion above with direct Postgres lock-manager
// state.
func assertBackendWaitingOnAdvisoryLock(t *testing.T, ctx context.Context, db *sql.DB, pid int) {
	t.Helper()
	var waiting int
	row := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pg_locks
WHERE pid = $1 AND locktype = 'advisory' AND NOT granted
`, pid)
	if err := row.Scan(&waiting); err != nil {
		t.Fatalf("read pg_locks for waiter backend: %v", err)
	}
	if waiting == 0 {
		t.Fatalf("pg_locks shows no ungranted advisory-lock entry for backend pid %d; want the waiter parked waiting on the per-user advisory lock", pid)
	}
}

// TestLocalIdentityMFAResetRaceWithoutLockDuplicatesActiveFactor
// reproduces the exact hazard lockLocalIdentityMFAReset exists to close:
// two transactions running ResetLocalIdentityMFA's revoke/insert statement
// sequence for the same user, without the lock, both observe zero active
// factors and both insert a replacement, landing two simultaneously active
// identity_mfa_factors rows. See the package doc comment above for why
// this cannot be reproduced by adding a barrier inside
// ResetLocalIdentityMFA itself while the lock is present (it would
// deadlock instead).
func TestLocalIdentityMFAResetRaceWithoutLockDuplicatesActiveFactor(t *testing.T) {
	dsn := localIdentityMFAResetProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_LOCAL_IDENTITY_MFA_RESET_PROOF_DSN or ESHU_POSTGRES_DSN to run the local-identity MFA reset race-reproduction gate")
	}

	ctx := context.Background()
	const rounds = 3
	for round := 0; round < rounds; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			ownerDB, schemaName := openLocalIdentityMFAResetSchemaFixture(t, ctx, dsn)
			runLocalIdentityMFAResetRaceWithoutLockRound(t, ctx, dsn, schemaName, ownerDB, round)
		})
	}
}

func runLocalIdentityMFAResetRaceWithoutLockRound(
	t *testing.T,
	ctx context.Context,
	dsn string,
	schemaName string,
	ownerDB *sql.DB,
	round int,
) {
	t.Helper()

	userID := fmt.Sprintf("user-mfa-race-nolock-%d", round)
	subjectIDHash := fmt.Sprintf("sha256:subject-mfa-race-nolock-%d", round)
	now := time.Now().UTC()
	seedLocalIdentityMFAResetFixtureUser(t, ctx, ownerDB, userID, subjectIDHash, now)

	// Both racers must reach the barrier — meaning both have already run
	// their revoke statements and observed the pre-reset state — before
	// either is allowed to insert its replacement factor. This forces the
	// exact "both saw zero, both inserted" interleaving deterministically,
	// instead of hoping two full ResetLocalIdentityMFA calls happen to
	// overlap.
	var barrier sync.WaitGroup
	barrier.Add(2)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := raceLocalIdentityMFAResetWithoutLock(ctx, dsn, schemaName, userID, i, round, &barrier); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	assertLocalIdentityMFAResetDuplicateActiveFactors(t, ctx, ownerDB, userID)
}

// raceLocalIdentityMFAResetWithoutLock runs the same revoke/insert
// statement sequence ResetLocalIdentityMFA runs
// (identity_local_lifecycle.go) — the same package-private query constants
// and the same insertLocalIdentityMFA helper — but deliberately omits the
// lockLocalIdentityMFAReset call and rendezvous at barrier between the
// revoke statements and the insert, so both racers interleave at exactly
// the point the advisory lock exists to guard. The barrier-release guard
// mirrors the commit/rollback guard pattern ResetLocalIdentityMFA itself
// uses, so exactly one Done() is emitted per call regardless of which
// step fails.
func raceLocalIdentityMFAResetWithoutLock(
	ctx context.Context,
	dsn, schemaName, userID string,
	racerIndex, round int,
	barrier *sync.WaitGroup,
) error {
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			barrier.Done()
		}
	}()

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("racer[%d] open: %w", racerIndex, err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		return fmt.Errorf("racer[%d] set search_path: %w", racerIndex, err)
	}

	tx, err := SQLDB{DB: conn}.Begin(ctx)
	if err != nil {
		return fmt.Errorf("racer[%d] begin: %w", racerIndex, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	resetAt := time.Now().UTC()

	// Deliberately no lockLocalIdentityMFAReset call here — this is the
	// hazard the production code's lock call exists to prevent.
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityRecoveryCodesQuery, userID, resetAt); err != nil {
		return fmt.Errorf("racer[%d] revoke recovery codes: %w", racerIndex, err)
	}
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityMFAFactorsQuery, userID, resetAt); err != nil {
		return fmt.Errorf("racer[%d] revoke mfa factors: %w", racerIndex, err)
	}

	// Barrier: both racers have now observed the pre-reset state (their
	// revoke statements ran and matched whatever was active at that
	// point). Neither proceeds to insert until both have crossed this
	// line.
	barrierReleased = true
	barrier.Done()
	barrier.Wait()

	if err := insertLocalIdentityMFA(
		ctx,
		tx,
		userID,
		fmt.Sprintf("factor-mfa-race-nolock-%d-racer-%d", round, racerIndex),
		"recovery_code",
		"",
		[]string{
			fmt.Sprintf("sha256:recovery-race-nolock-%d-racer-%d-a", round, racerIndex),
			fmt.Sprintf("sha256:recovery-race-nolock-%d-racer-%d-b", round, racerIndex),
		},
		resetAt,
	); err != nil {
		return fmt.Errorf("racer[%d] insert mfa: %w", racerIndex, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("racer[%d] commit: %w", racerIndex, err)
	}
	committed = true
	return nil
}

// assertLocalIdentityMFAResetDuplicateActiveFactors proves the hazard: with
// the advisory lock skipped and the barrier forcing both racers past their
// revoke statements before either inserts, exactly two active
// identity_mfa_factors rows survive for the user — the duplicate-active-
// factor state lockLocalIdentityMFAReset exists to prevent.
func assertLocalIdentityMFAResetDuplicateActiveFactors(
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
	if activeFactors != 2 {
		t.Fatalf("active identity_mfa_factors rows for user with the advisory lock skipped = %d, want exactly 2 (the barrier-forced interleaving must reproduce the duplicate-active-factor hazard)", activeFactors)
	}
}
