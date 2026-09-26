// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestProjectorClaimDoesNotWaitOnScopeUpdateChain reproduces the wait that
// turned into Claim/Ack deadlocks when the #7115 fence lived on
// ingestion_scopes. The claimer's snapshot sees scope-s at version v1 while:
//   - K holds v1 FOR KEY SHARE, the lock an FK child insert takes;
//   - U1 commits a non-key UPDATE of scope-s (v1 -> v2), as an earlier Ack did;
//   - A2 runs Ack's first statement (updateProjectorScopeGenerationQuery) on
//     v2 and stays open.
//
// With a running KEY SHARE member and a committed updater on v1, locking v1
// walks the update chain with a blocking wait that SKIP LOCKED does not cover,
// so a claim that locks ingestion_scopes waits on A2 while holding the work
// row A2's next statement needs. The claim must instead return while A2 is
// still open: it reclaims gen-s1's expired lease, and A2's Ack for the old
// attempt then matches no row (ErrProjectorClaimRejected in ProjectorQueue).
// No deadlock may be recorded.
func TestProjectorClaimDoesNotWaitOnScopeUpdateChain(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 6)
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
	// gen-s1's lease expired, so it is the claimer's first candidate.
	seedClaimMaintenanceWork(t, database, "scope-s", "gen-s1", "pending", "claimed", time.Hour, durationPtr(-time.Minute))
	// gen-z1 is supersedable, so the claim's supersede UPDATE fires the pause.
	seedClaimMaintenanceWork(t, database, "scope-z", "gen-z1", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-z", "gen-z2", "pending", "pending", 2*time.Hour, nil)
	ctx := context.Background()
	deadlocksBefore := serverDeadlockCount(t, database)

	keyShare, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin key-share holder: %v", err)
	}
	defer func() { _ = keyShare.Rollback() }()
	if _, err := keyShare.Exec(`SELECT 1 FROM ingestion_scopes WHERE scope_id = 'scope-s' FOR KEY SHARE`); err != nil {
		t.Fatalf("key-share lock scope-s: %v", err)
	}

	paused := openPausedClaimPool(t, database, dsn)
	type claimResult struct {
		generation string
		attempt    int
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-c", time.Minute)
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, work.AttemptCount, ok, err}
	}()
	waitForPausedClaimer(t, dsn)

	if _, err := database.Exec(
		`UPDATE ingestion_scopes SET ingested_at = ingested_at + interval '1 second' WHERE scope_id = 'scope-s'`,
	); err != nil {
		t.Fatalf("committed scope-s update: %v", err)
	}
	ack, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin Ack-shaped transaction: %v", err)
	}
	defer func() { _ = ack.Rollback() }()
	now := time.Now().UTC()
	if _, err := ack.Exec(updateProjectorScopeGenerationQuery, now, "scope-s", "gen-s1"); err != nil {
		t.Fatalf("Ack scope update: %v", err)
	}

	result, waiting := awaitClaimOrLockWait(t, dsn, done)
	if waiting {
		// RED: observed, not inferred. Run Ack's second statement to show the
		// cycle the field reports, then fail.
		_, ackErr := ack.Exec(ackProjectorWorkItemQuery, now, "scope-s", "gen-s1", "other-worker", 1)
		_ = ack.Rollback()
		result = <-done
		t.Fatalf("claim waited on the open scope updater (Lock/transactionid); Ack work update err = %v (40P01 %v), claim = (%q, %v, %v, 40P01 %v)",
			ackErr, isDeadlock(ackErr), result.generation, result.ok, result.err, isDeadlock(result.err))
	}
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if !result.ok || result.generation != "gen-s1" || result.attempt != 2 {
		t.Fatalf("paused Claim() = (%q, attempt %d, %v) while the Ack is open, want gen-s1 attempt 2",
			result.generation, result.attempt, result.ok)
	}
	ackResult, err := ack.Exec(ackProjectorWorkItemQuery, now, "scope-s", "gen-s1", "other-worker", 1)
	if err != nil {
		t.Fatalf("Ack work update error = %v, want the reclaimed attempt rejected", err)
	}
	if rows, _ := ackResult.RowsAffected(); rows != 0 {
		t.Fatalf("Ack of the reclaimed attempt updated %d rows, want 0 (claim rejected)", rows)
	}
	if err := ack.Commit(); err != nil {
		t.Fatalf("commit Ack-shaped transaction: %v", err)
	}
	time.Sleep(1500 * time.Millisecond) // let backends flush pg_stat counters
	if delta := serverDeadlockCount(t, database) - deadlocksBefore; delta != 0 {
		t.Fatalf("server recorded %d deadlocks, want 0", delta)
	}
}

// awaitClaimOrLockWait waits up to 6 s for the paused claim to return, polling
// pg_stat_activity for its session. It reports waiting=true as soon as the
// session shows a heavyweight transactionid lock wait.
func awaitClaimOrLockWait[T any](t *testing.T, dsn string, done chan T) (T, bool) {
	t.Helper()
	observer, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open observer: %v", err)
	}
	defer func() { _ = observer.Close() }()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case result := <-done:
			return result, false
		default:
		}
		var waiting bool
		if err := observer.QueryRow(`
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE application_name = $1
      AND wait_event_type = 'Lock'
      AND wait_event = 'transactionid'
)`, pausedClaimApplicationName).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting {
			var zero T
			return zero, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("claim neither returned nor showed a lock wait within 6 s")
	var zero T
	return zero, false
}

// isDeadlock reports whether err carries SQLSTATE 40P01.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// TestProjectorClaimFenceRowNeverMakesClaimersWait proves claimers cannot
// wait on each other through the fence row, so they cannot form a cycle on
// it. C2's snapshot predates C1, which locks the scope-s fence row and bumps
// it exactly as the claim does, then commits (v1 -> v2). C3 is still in
// flight on v2, holding it either lock-only or with its bump applied. C2 must
// return without a lock wait while C3 is open, and must not claim in scope-s:
// its snapshot fence no longer matches.
func TestProjectorClaimFenceRowNeverMakesClaimersWait(t *testing.T) {
	for _, tc := range []struct {
		name   string
		c3Bump bool
	}{
		{"in_flight_claimer_lock_only", false},
		{"in_flight_claimer_bumped", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := claimMaintenanceProofDSN(t)
			database := openClaimDeadlockProofDB(t, dsn, 6)
			seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
			seedClaimMaintenanceWork(t, database, "scope-s", "gen-s", "pending", "pending", time.Hour, nil)
			seedClaimMaintenanceWork(t, database, "scope-z", "gen-z1", "pending", "pending", 3*time.Hour, nil)
			seedClaimMaintenanceWork(t, database, "scope-z", "gen-z2", "pending", "pending", 2*time.Hour, nil)
			ctx := context.Background()

			paused := openPausedClaimPool(t, database, dsn)
			type claimResult struct {
				generation string
				ok         bool
				err        error
			}
			done := make(chan claimResult, 1)
			go func() {
				queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-c2", time.Minute)
				work, ok, err := queue.Claim(context.Background())
				done <- claimResult{work.Generation.GenerationID, ok, err}
			}()
			waitForPausedClaimer(t, dsn)

			bumpFence := func(tx *sql.Tx, who string, bump bool) {
				t.Helper()
				if _, err := tx.Exec(`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-s' FOR NO KEY UPDATE`); err != nil {
					t.Fatalf("%s lock fence: %v", who, err)
				}
				if !bump {
					return
				}
				if _, err := tx.Exec(`UPDATE projector_scope_claim_fences SET fence = fence + 1 WHERE scope_id = 'scope-s'`); err != nil {
					t.Fatalf("%s bump fence: %v", who, err)
				}
			}
			c1, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin c1: %v", err)
			}
			bumpFence(c1, "c1", true)
			if err := c1.Commit(); err != nil {
				t.Fatalf("commit c1: %v", err)
			}
			c3, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin c3: %v", err)
			}
			defer func() { _ = c3.Rollback() }()
			bumpFence(c3, "c3", tc.c3Bump)

			result, waiting := awaitClaimOrLockWait(t, dsn, done)
			if waiting {
				_ = c3.Rollback()
				result = <-done
				t.Fatalf("C2 waited on in-flight C3 through the fence row; returned (%q, %v, %v) after C3 ended",
					result.generation, result.ok, result.err)
			}
			if result.err != nil || !result.ok || result.generation != "gen-z2" {
				t.Fatalf("C2 = (%q, %v, %v) with C3 open, want gen-z2", result.generation, result.ok, result.err)
			}
		})
	}
}
