// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease
// proves the #4464 Bug 2 TOCTOU fix's runtime effect against a real Postgres
// instance, not just the SQL text. Set ESHU_GENERATION_LIVENESS_PROOF_DSN to
// run it; it is skipped otherwise, matching the sibling proof in
// generation_liveness_integration_test.go.
//
// The wedged-generations CTE reads the projector row's lease state from a
// snapshot taken before the ON CONFLICT DO UPDATE executes. Between that
// snapshot and the UPDATE, a live worker's Heartbeat can extend claim_until
// (or a fresh Claim() can move status to claimed/running), making the row
// genuinely in-flight again by the time the UPDATE actually runs. The write-
// time WHERE guard added in generation_liveness_sql.go must re-check the same
// in-flight condition at write time so this concurrently-renewed claim is not
// clobbered back to pending.
//
// This is proven here, not just asserted by string-matching the SQL, by
// racing two real transactions against the same row:
//
//  1. A "heartbeat" transaction BEGINs, UPDATEs the row's claim_until to a
//     future time (renewing the lease), and blocks before COMMIT.
//  2. RecoverWedgedGenerations runs concurrently. Postgres's own row-level
//     locking forces it to block on the INSERT ... ON CONFLICT DO UPDATE
//     until the heartbeat transaction's row lock releases.
//  3. The heartbeat transaction commits.
//  4. RecoverWedgedGenerations's write-time WHERE guard re-evaluates against
//     the now-committed, now-live claim_until and must skip the row.
//
// Without the write-time guard, RecoverWedgedGenerations would unconditionally
// clobber the row back to status=pending/lease_owner=NULL/claim_until=NULL
// the instant the heartbeat's lock released, discarding the concurrently
// renewed claim.
func TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness write-time race proof")
	}

	// Two independent connections are required: the sweep and the heartbeat
	// must be able to hold locks concurrently, which a single-connection pool
	// (as used by the read-only fixture helpers) cannot do.
	sweepDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sweep connection: %v", err)
	}
	t.Cleanup(func() { _ = sweepDB.Close() })
	sweepDB.SetMaxOpenConns(1)

	heartbeatDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open heartbeat connection: %v", err)
	}
	t.Cleanup(func() { _ = heartbeatDB.Close() })
	heartbeatDB.SetMaxOpenConns(1)

	provisionLivenessSchema(t, sweepDB, generationLivenessExpiredLeaseOnlySeedSQL)
	ctx := context.Background()

	// Both connections must share the sweep connection's isolated proof
	// schema: provisionLivenessSchema sets search_path only on sweepDB, so
	// point heartbeatDB at the same schema explicitly.
	var schemaName string
	if err := sweepDB.QueryRowContext(ctx, "SHOW search_path").Scan(&schemaName); err != nil {
		t.Fatalf("read sweep search_path: %v", err)
	}
	if _, err := heartbeatDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set heartbeat search_path: %v", err)
	}

	// Step 1: the heartbeat transaction begins and renews the lease, then
	// blocks (holding the row lock) until the test signals it to commit.
	heartbeatTx, err := heartbeatDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin heartbeat tx: %v", err)
	}
	// Guarantee the row lock is released on every exit path, including an
	// early t.Fatalf between here and the intentional Commit() below. An
	// open, uncommitted heartbeatTx holding this row lock would otherwise
	// outlive the test and can hang a later proof test that reuses the same
	// DSN. Rollback after a successful Commit is a harmless no-op
	// (sql.ErrTxDone), so this is safe to defer unconditionally.
	defer func() { _ = heartbeatTx.Rollback() }()
	renewedClaimUntil := time.Now().UTC().Add(10 * time.Minute)
	if _, err := heartbeatTx.ExecContext(ctx, `
		UPDATE fact_work_items
		SET claim_until = $1
		WHERE scope_id = 'scope-expired-lease'
		  AND generation_id = 'gen-expired-lease'
		  AND stage = 'projector'
		  AND domain = 'source_local'
	`, renewedClaimUntil); err != nil {
		t.Fatalf("heartbeat renew claim_until: %v", err)
	}

	// Step 2: start the sweep on the other connection. It must block on the
	// row lock the heartbeat transaction holds, so run it in a goroutine and
	// prove it has not returned yet before committing the heartbeat.
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 1,
		BatchLimit:         100,
	}
	store := NewGenerationLivenessStore(SQLDB{DB: sweepDB})
	now := time.Now().UTC()

	type sweepOutcome struct {
		result GenerationLivenessResult
		err    error
	}
	sweepDone := make(chan sweepOutcome, 1)
	go func() {
		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		sweepDone <- sweepOutcome{result: result, err: err}
	}()

	select {
	case outcome := <-sweepDone:
		t.Fatalf(
			"sweep returned before the heartbeat transaction committed (result=%+v err=%v) — "+
				"it should have blocked on the row lock the heartbeat holds",
			outcome.result, outcome.err,
		)
	case <-time.After(200 * time.Millisecond):
		// Expected: the sweep is blocked on the row lock.
	}

	// Step 3: commit the heartbeat. The sweep's blocked UPDATE can now
	// proceed and must re-evaluate the write-time WHERE guard against the
	// just-committed, now-live claim_until.
	if err := heartbeatTx.Commit(); err != nil {
		t.Fatalf("commit heartbeat tx: %v", err)
	}

	var outcome sweepOutcome
	select {
	case outcome = <-sweepDone:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not complete within 5s after heartbeat commit")
	}
	if outcome.err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v", outcome.err)
	}

	// Step 4: the row must be untouched by the sweep — still carrying the
	// heartbeat's renewed claim_until, not clobbered to pending.
	if outcome.result.Recovered != 0 {
		t.Fatalf(
			"Recovered = %d, want 0 (the write-time guard must skip a lease renewed while the sweep was blocked on it)",
			outcome.result.Recovered,
		)
	}

	var status string
	var leaseOwner sql.NullString
	var claimUntil sql.NullTime
	if err := sweepDB.QueryRowContext(ctx, `
		SELECT status, lease_owner, claim_until
		FROM fact_work_items
		WHERE scope_id = 'scope-expired-lease'
		  AND generation_id = 'gen-expired-lease'
		  AND stage = 'projector'
		  AND domain = 'source_local'
	`).Scan(&status, &leaseOwner, &claimUntil); err != nil {
		t.Fatalf("query race-tested projector work: %v", err)
	}
	if status != "claimed" {
		t.Fatalf("status = %q, want claimed (the sweep must not have clobbered the renewed claim)", status)
	}
	if !leaseOwner.Valid || leaseOwner.String != "bootstrap-index" {
		t.Fatalf("lease_owner = %v, want 'bootstrap-index' (untouched)", leaseOwner)
	}
	if !claimUntil.Valid || !claimUntil.Time.Equal(renewedClaimUntil) {
		t.Fatalf("claim_until = %v, want the heartbeat-renewed %v (must not be cleared)", claimUntil, renewedClaimUntil)
	}
}
