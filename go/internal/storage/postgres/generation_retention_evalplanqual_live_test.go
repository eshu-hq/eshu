// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"
)

// generationRetentionCandidateQueryBlockingLock derives, from the shipped
// constant, the final SELECT's exact lock clause with SKIP LOCKED removed.
// Production never blocks: SKIP LOCKED only changes behavior while another
// transaction HOLDS the row lock, not after it commits and releases
// (TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow uses the same
// technique for the reducer queue's lock CTE), so a blocking wait here forces
// the same EvalPlanQual recheck deterministically instead of racing real
// timing against a live session.
var generationRetentionCandidateQueryBlockingLock = strings.Replace(
	generationRetentionCandidateQuery,
	"FOR UPDATE OF generation, scope SKIP LOCKED",
	"FOR UPDATE OF generation, scope",
	1,
)

func init() {
	if generationRetentionCandidateQueryBlockingLock == generationRetentionCandidateQuery {
		panic("generationRetentionCandidateQueryBlockingLock: shipped query text changed, the derived blocking-lock mirror no longer differs")
	}
}

// TestGenerationRetentionEvalPlanQualDropsRacedCandidatesLive is P5: a
// candidate generation that is deleted-and-committed, or whose status is
// changed away from 'superseded' and committed, by another session between
// the candidate query's snapshot and the final SELECT's row lock must be
// dropped, not returned as a locked candidate.
func TestGenerationRetentionEvalPlanQualDropsRacedCandidatesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	cutoff := now.Add(-7 * 24 * time.Hour)
	hardCutoff := now.Add(-90 * 24 * time.Hour)
	old := now.Add(-10 * 24 * time.Hour)

	t.Run("pruned-and-committed-by-another-session", func(t *testing.T) {
		scopeID, generationID := "epq-del", "epq-del-g0"
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, generationID, old)

		locked := runGenerationRetentionRaceCase(t, ctx, database, generationID, cutoff, hardCutoff, func(holder *sql.Tx) {
			if _, err := holder.ExecContext(context.Background(), `DELETE FROM scope_generations WHERE generation_id = $1`, generationID); err != nil {
				t.Fatalf("concurrent delete: %v", err)
			}
		})
		if len(locked) != 0 {
			t.Fatalf("locked rows = %v, want none: a generation deleted and committed by another session must be dropped by EvalPlanQual", locked)
		}
	})

	t.Run("reactivated-by-another-session", func(t *testing.T) {
		scopeID, generationID := "epq-react", "epq-react-g0"
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, generationID, old)

		// 'pending', not 'active': scope_generations_active_scope_idx allows
		// only one 'active' row per scope, and this fixture's scope already
		// has one (its own active generation). Any non-'superseded' status
		// exercises the same EvalPlanQual recheck on generation.status.
		locked := runGenerationRetentionRaceCase(t, ctx, database, generationID, cutoff, hardCutoff, func(holder *sql.Tx) {
			if _, err := holder.ExecContext(context.Background(), `UPDATE scope_generations SET status = 'pending' WHERE generation_id = $1`, generationID); err != nil {
				t.Fatalf("concurrent reactivate: %v", err)
			}
		})
		if len(locked) != 0 {
			t.Fatalf("locked rows = %v, want none: a generation reactivated and committed by another session must be dropped by EvalPlanQual", locked)
		}
	})

	// Baseline, no race: a lone eligible candidate is both selected and
	// pruned once, through the real store, so it gets exactly one retention
	// event. Combined with the two subtests above (which prove a raced
	// candidate never reaches the `candidates` slice PruneSupersededGenerations
	// iterates to write events), this covers "one event per generation": a
	// generation the race drops gets zero events, a survivor gets exactly one.
	t.Run("bystander-gets-exactly-one-event", func(t *testing.T) {
		scopeID, generationID := "epq-bystander", "epq-bystander-g0"
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, generationID, old)

		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(0, 7*24*time.Hour, 10))
		if err != nil {
			t.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		if result.GenerationsPruned != 1 {
			t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
		}
		var events int
		if err := database.QueryRowContext(ctx,
			`SELECT count(*) FROM generation_retention_events WHERE generation_id_hash = $1`,
			retentionHashID("generation", generationID),
		).Scan(&events); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if events != 1 {
			t.Fatalf("events for %s = %d, want 1", generationID, events)
		}
	})
}

// runGenerationRetentionRaceCase holds generationID's row FOR UPDATE from a
// second connection, starts the blocking-lock mirror of the candidate query
// (which must then block waiting for that row), lets it settle into the
// wait, applies race (the concurrent write) and commits the holder, then
// waits for the mirror query and returns the generation ids it ended up
// locking.
func runGenerationRetentionRaceCase(t *testing.T, ctx context.Context, database *sql.DB, generationID string, cutoff, hardCutoff time.Time, race func(holder *sql.Tx)) []string {
	t.Helper()
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `SELECT generation_id FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, generationID); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	var (
		wg      sync.WaitGroup
		locked  []string
		lockErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, generationRetentionCandidateQueryBlockingLock, cutoff, 0, 10, hardCutoff) // blocks on the holder
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var scopeID, gotGenerationID, scopeKind string
			var supersededAt, observedAt time.Time
			if err := rows.Scan(&scopeID, &gotGenerationID, &scopeKind, &supersededAt, &observedAt); err != nil {
				lockErr = err
				return
			}
			locked = append(locked, gotGenerationID)
		}
		lockErr = rows.Err()
	}()

	// Let the mirror query block on the holder's row lock before racing it.
	// A fixed sleep can pass without the mirror ever reaching its blocking
	// lock on a loaded host: it would then take a fresh post-commit
	// snapshot, filter the raced row out on its own, and return zero rows,
	// so the assertions below would pass without ever exercising the
	// EvalPlanQual recheck this test exists to prove. Poll for the mirror's
	// own backend actually waiting on a lock instead.
	waitForMirrorLockWait(t, ctx, database)
	race(holder)
	if err := holder.Commit(); err != nil {
		t.Fatalf("holder commit: %v", err)
	}

	wg.Wait()
	if lockErr != nil {
		t.Fatalf("mirror query: %v", lockErr)
	}
	return locked
}

// waitForMirrorLockWait polls pg_stat_activity for a backend other than this
// one blocked on a lock while running a query against scope_generations: the
// mirror goroutine's blocking-lock variant of the candidate query, actually
// waiting on the holder's row lock rather than merely dispatched. Fails the
// test if that never happens within the deadline, so the race case cannot
// silently skip the wait it exists to force.
func waitForMirrorLockWait(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var blocked int
		if err := database.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity
			 WHERE pid <> pg_backend_pid()
			   AND datname = current_database()
			   AND wait_event_type = 'Lock'
			   AND query LIKE '%scope_generations%'`,
		).Scan(&blocked); err != nil {
			t.Fatalf("poll mirror wait: %v", err)
		}
		if blocked > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("mirror query never blocked on the holder row lock")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
