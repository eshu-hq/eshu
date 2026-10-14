// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestProjectorClaimDoesNotWaitOnGenerationUpdateChain reproduces the wait
// that puts the claim into a deadlock with Ack when the maintenance sweep
// locks scope_generations (#7187). The claimer fixes a repeatable-read
// snapshot that sees gen-s1 at version v1 (the production claimer's
// statement snapshot predating a committed update), while:
//   - K holds v1 FOR KEY SHARE, the lock an FK child insert takes;
//   - U1 commits a non-key UPDATE of gen-s1 (v1 -> v2);
//   - A2 runs Ack's obsolete-generation maintenance on v2 (fence row first,
//     then the generation UPDATE, as the fixed Ack does) and stays open.
//
// With a running KEY SHARE member and a committed updater on v1, locking v1
// walks the update chain with a blocking wait that SKIP LOCKED does not
// cover, so a claim that locks the generation row waits on A2. The fixed
// claim locks each stale scope's claim fence first (a claim-only target with
// no FK children, so no chain can form under it) and skips a fenced-busy
// scope without touching its generations: it reclaims gen-s0's expired
// lease, sweeps scope-z, and claims gen-z2 while A2 is still open. No
// deadlock may be recorded.
func TestProjectorClaimDoesNotWaitOnGenerationUpdateChain(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 6)
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
	// gen-s0's lease expired beside gen-sL's live one, so the duplicate
	// reclaim fires. gen-s1 is the chain target: pending with newer gen-s2.
	seedClaimMaintenanceWork(t, database, "scope-s", "gen-s0", "pending", "claimed", 4*time.Hour, durationPtr(-time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-s", "gen-sL", "active", "running", 4*time.Hour, durationPtr(time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-s", "gen-s1", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-s", "gen-s2", "pending", "pending", 2*time.Hour, nil)
	// gen-z1 is supersedable, so the fixed claim still sweeps one scope and
	// claims gen-z2 while scope-s is fenced busy.
	seedClaimMaintenanceWork(t, database, "scope-z", "gen-z1", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-z", "gen-z2", "pending", "pending", 2*time.Hour, nil)
	ctx := context.Background()
	deadlocksBefore := serverDeadlockCount(t, database)

	keyShare, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin key-share holder: %v", err)
	}
	defer func() { _ = keyShare.Rollback() }()
	if _, err := keyShare.Exec(`SELECT 1 FROM scope_generations WHERE generation_id = 'gen-s1' FOR KEY SHARE`); err != nil {
		t.Fatalf("key-share lock gen-s1: %v", err)
	}

	// The claimer fixes its snapshot at v1 before the committed update,
	// exactly like a claim statement whose snapshot predates U1.
	claimer, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("begin claimer: %v", err)
	}
	defer func() { _ = claimer.Rollback() }()
	if _, err := claimer.Exec(`SELECT 1 FROM scope_generations WHERE generation_id = 'gen-s1'`); err != nil {
		t.Fatalf("fix claimer snapshot: %v", err)
	}
	// Tag the session so the lock-wait poll finds it. SET LOCAL reverts at
	// transaction end, so the pooled connection is unaffected.
	if _, err := claimer.Exec(`SET LOCAL application_name = '` + pausedClaimApplicationName + `'`); err != nil {
		t.Fatalf("tag claimer session: %v", err)
	}

	if _, err := database.Exec(
		`UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = 'gen-s1'`,
	); err != nil {
		t.Fatalf("committed gen-s1 update: %v", err)
	}
	ack, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin Ack-shaped transaction: %v", err)
	}
	defer func() { _ = ack.Rollback() }()
	if _, err := ack.Exec(`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-s' FOR NO KEY UPDATE`); err != nil {
		t.Fatalf("Ack fence lock: %v", err)
	}
	now := time.Now().UTC()
	// Holds the chain tip the way Ack's obsolete-generation UPDATE does. A
	// status write would pollute the skip assertions below once committed,
	// so this bumps ingested_at: it still takes the row lock and opens a new
	// version for the chain walk to wait on.
	if _, err := ack.Exec(
		`UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = 'gen-s1'`,
	); err != nil {
		t.Fatalf("Ack generation update: %v", err)
	}

	type claimResult struct {
		generation string
		attempt    int
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLTx{Tx: claimer}, "worker-c", time.Minute)
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, work.AttemptCount, ok, err}
	}()

	result, waiting := awaitClaimOrLockWait(t, dsn, done)
	if waiting {
		// RED: observed, not inferred. Run Ack's next statement against the
		// duplicate row the claim may hold to exhibit the cycle, then unwind
		// and fail. The timer bounds a mutual wait: the detector fires fast
		// on a true cycle, otherwise the rollback below releases both sides.
		stmt2done := make(chan error, 1)
		go func() {
			_, err := ack.Exec(ackProjectorWorkItemQuery, now, "scope-s", "gen-s0", "other-worker", 1)
			stmt2done <- err
		}()
		timer := time.NewTimer(8 * time.Second)
		var ackErr error
		gotStmt2 := false
		select {
		case ackErr = <-stmt2done:
			gotStmt2 = true
		case <-timer.C:
		}
		_ = ack.Rollback()
		if !gotStmt2 {
			timer.Reset(5 * time.Second)
			select {
			case ackErr = <-stmt2done:
			case <-timer.C:
				t.Fatalf("Ack second statement still blocked after rollback; the claim wedged the test")
			}
		}
		result = <-done
		t.Fatalf("claim waited on the open generation updater (Lock/transactionid); Ack work update err = %v (40P01 %v), claim = (%q, %v, %v, 40P01 %v)",
			ackErr, isDeadlock(ackErr), result.generation, result.ok, result.err, isDeadlock(result.err))
	}
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if !result.ok || result.generation != "gen-z2" || result.attempt != 2 {
		t.Fatalf("paused Claim() = (%q, attempt %d, %v) while the Ack is open, want gen-z2 attempt 2",
			result.generation, result.attempt, result.ok)
	}
	if err := claimer.Commit(); err != nil {
		t.Fatalf("commit claimer: %v", err)
	}
	// gen-s0's expired lease was reclaimed, so Ack's update for the old
	// attempt matches no row.
	ackResult, err := ack.Exec(ackProjectorWorkItemQuery, now, "scope-s", "gen-s0", "other-worker", 1)
	if err != nil {
		t.Fatalf("Ack work update error = %v, want the reclaimed attempt rejected", err)
	}
	if rows, _ := ackResult.RowsAffected(); rows != 0 {
		t.Fatalf("Ack of the reclaimed attempt updated %d rows, want 0 (claim rejected)", rows)
	}
	if status, _, _ := workState(t, database, "scope-s", "gen-s0"); status != "retrying" {
		t.Fatalf("gen-s0 work = %q, want retrying (reclaimed while scope-s was fenced)", status)
	}
	// scope-s was skipped while fenced busy: its stale generation is
	// untouched, and the next claim retires it once the fence is free.
	if status, _, _ := workState(t, database, "scope-s", "gen-s1"); status != "pending" {
		t.Fatalf("gen-s1 work = %q while scope-s was fenced, want pending (sweep skips, not steals)", status)
	}
	if err := ack.Commit(); err != nil {
		t.Fatalf("commit Ack-shaped transaction: %v", err)
	}
	work, ok, err := NewProjectorQueue(SQLDB{DB: database}, "worker-c", time.Minute).Claim(ctx)
	if err != nil || ok {
		t.Fatalf("follow-up Claim() = (%q, %v, %v), want no claim (scope-s live lease, scope-z claimed)",
			work.Generation.GenerationID, ok, err)
	}
	if status, _, _ := workState(t, database, "scope-s", "gen-s1"); status != "superseded" {
		t.Fatalf("gen-s1 work after release = %q, want superseded (skip defers, not drops)", status)
	}
	if got := generationState(t, database, "gen-s1"); got != "superseded" {
		t.Fatalf("gen-s1 generation after release = %q, want superseded", got)
	}
	time.Sleep(1500 * time.Millisecond) // let backends flush pg_stat counters
	if delta := serverDeadlockCount(t, database) - deadlocksBefore; delta != 0 {
		t.Fatalf("server recorded %d deadlocks, want 0", delta)
	}
}
