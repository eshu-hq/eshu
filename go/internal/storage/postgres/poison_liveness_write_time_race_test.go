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

// TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim proves the
// #4464-grade write-time re-verify guard against a real Postgres instance, not
// just the SQL text. Set ESHU_POISON_LIVENESS_PROOF_DSN to run it; skipped
// otherwise, mirroring generation_liveness_write_time_race_test.go.
//
// The poison candidate CTE reads a snapshot of dead_letter rows before the
// UPDATE executes. Between that snapshot and the UPDATE's row lock, a
// concurrent worker can reclaim the exact same row (dead_letter -> claimed) —
// for example a manual operator replay, or another liveness sweep instance.
// The write-time "AND target.status = 'dead_letter'" guard in
// recoverPoisonDeadLettersQuery must re-check that condition at write time so
// this concurrently-reclaimed row is NOT clobbered back to pending.
//
// This is proven by racing two real transactions against the same row:
//
//  1. A "reclaim" transaction BEGINs, UPDATEs the row's status to 'claimed'
//     (simulating a concurrent worker or operator reclaim), and blocks before
//     COMMIT.
//  2. RecoverPoisonDeadLetters runs concurrently. Postgres's own row-level
//     locking forces its UPDATE to block on the row lock the reclaim
//     transaction holds.
//  3. The reclaim transaction commits.
//  4. RecoverPoisonDeadLetters's write-time WHERE guard re-evaluates against
//     the now-committed, now-claimed row (EvalPlanQual recheck) and must skip
//     it — affects zero rows for that row, final state is claimed, not
//     clobbered back to pending.
func TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim(t *testing.T) {
	dsn := os.Getenv("ESHU_POISON_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POISON_LIVENESS_PROOF_DSN to run the poison liveness write-time race proof")
	}

	// Two independent connections are required: the sweep and the reclaim
	// transaction must be able to hold locks concurrently, which a
	// single-connection pool cannot do.
	sweepDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sweep connection: %v", err)
	}
	t.Cleanup(func() { _ = sweepDB.Close() })
	sweepDB.SetMaxOpenConns(1)

	reclaimDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open reclaim connection: %v", err)
	}
	t.Cleanup(func() { _ = reclaimDB.Close() })
	reclaimDB.SetMaxOpenConns(1)

	provisionPoisonLivenessSchema(t, sweepDB, poisonLivenessRaceSeedSQL)
	ctx := context.Background()

	// Both connections must share the sweep connection's isolated proof
	// schema: provisionPoisonLivenessSchema sets search_path only on sweepDB.
	var schemaName string
	if err := sweepDB.QueryRowContext(ctx, "SHOW search_path").Scan(&schemaName); err != nil {
		t.Fatalf("read sweep search_path: %v", err)
	}
	if _, err := reclaimDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set reclaim search_path: %v", err)
	}

	// Step 1: the reclaim transaction begins, moves the row to 'claimed' (a
	// concurrent worker taking ownership), and blocks (holding the row lock)
	// until the test signals it to commit.
	reclaimTx, err := reclaimDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin reclaim tx: %v", err)
	}
	// Guarantee the row lock is released on every exit path. Rollback after a
	// successful Commit is a harmless no-op (sql.ErrTxDone).
	defer func() { _ = reclaimTx.Rollback() }()
	if _, err := reclaimTx.ExecContext(ctx, `
		UPDATE fact_work_items
		SET status = 'claimed', lease_owner = 'worker-a', claim_until = $1
		WHERE work_item_id = 'wi-poison-race'
	`, time.Now().UTC().Add(10*time.Minute)); err != nil {
		t.Fatalf("reclaim update: %v", err)
	}

	// Step 2: start the sweep on the other connection. It must block on the
	// row lock the reclaim transaction holds.
	store := NewPoisonLivenessStore(SQLDB{DB: sweepDB})
	policy := PoisonLivenessPolicy{MaxRecoverAttempts: 1, BatchLimit: 100}
	now := time.Now().UTC()

	type sweepOutcome struct {
		result PoisonRecoveryResult
		err    error
	}
	sweepDone := make(chan sweepOutcome, 1)
	go func() {
		result, err := store.RecoverPoisonDeadLetters(ctx, policy, now)
		sweepDone <- sweepOutcome{result: result, err: err}
	}()

	select {
	case outcome := <-sweepDone:
		t.Fatalf(
			"sweep returned before the reclaim transaction committed (result=%+v err=%v) — "+
				"it should have blocked on the row lock the reclaim holds",
			outcome.result, outcome.err,
		)
	case <-time.After(200 * time.Millisecond):
		// Expected: the sweep is blocked on the row lock.
	}

	// Step 3: commit the reclaim. The sweep's blocked UPDATE can now proceed
	// and must re-evaluate the write-time WHERE guard against the
	// just-committed, now-claimed row.
	if err := reclaimTx.Commit(); err != nil {
		t.Fatalf("commit reclaim tx: %v", err)
	}

	var outcome sweepOutcome
	select {
	case outcome = <-sweepDone:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep did not complete within 5s after reclaim commit")
	}
	if outcome.err != nil {
		t.Fatalf("RecoverPoisonDeadLetters() error = %v", outcome.err)
	}

	// Step 4: the row must be untouched by the sweep — still claimed by
	// worker-a, not clobbered back to pending, and its recovery-attempt budget
	// must NOT have been incremented (the sweep affected zero rows for it).
	if outcome.result.Recovered != 0 {
		t.Fatalf(
			"Recovered = %d, want 0 (the write-time guard must skip a row reclaimed while the sweep was blocked on it)",
			outcome.result.Recovered,
		)
	}

	var status string
	var leaseOwner sql.NullString
	var attempts sql.NullInt64
	if err := sweepDB.QueryRowContext(ctx, `
		SELECT status, lease_owner, (payload ->> 'poison_recovery_attempts')::int
		FROM fact_work_items WHERE work_item_id = 'wi-poison-race'
	`).Scan(&status, &leaseOwner, &attempts); err != nil {
		t.Fatalf("query race-tested row: %v", err)
	}
	if status != "claimed" {
		t.Fatalf("status = %q, want claimed (the sweep must not have clobbered the reclaim)", status)
	}
	if !leaseOwner.Valid || leaseOwner.String != "worker-a" {
		t.Fatalf("lease_owner = %v, want 'worker-a' (untouched)", leaseOwner)
	}
	if attempts.Valid && attempts.Int64 != 0 {
		t.Fatalf("poison_recovery_attempts = %v, want unset/0 (the sweep must not have written to this row)", attempts)
	}
}

// poisonLivenessRaceSeedSQL seeds one isolated poison scope/row for the
// concurrency race proof, with no competing rows so the sweep's batch limit
// and candidate selection are unambiguous.
const poisonLivenessRaceSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('scope-poison-race', 'repo', 'git', 'kr', 'git', 'pr', now(), now(), 'active', NULL);

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
    ('gen-poison-race', 'scope-poison-race', 'push', now() - interval '1 hour', now() - interval '1 hour', 'failed', NULL);

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    payload, created_at, updated_at
) VALUES
    ('wi-poison-race', 'scope-poison-race', 'gen-poison-race', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour');
`
