// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// markGuardClaimRaceSeed is the #7469 retry-claim-vs-newer-claim fixture:
// gen-il1 is a marked retrying row with the newer pending gen-il2 beside it.
// Each iteration rebases gen-il1's visible_at so racer B's clock always sees
// it invisible while racer A's clock always sees it visible.
const markGuardClaimRaceSeed = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ('scope-il', 'repository', 'git', 'scope-il', 'git',
          'scope-il', now(), now(), 'active');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    projection_write_started_at
) VALUES ('gen-il1', 'scope-il', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'pending', now() - interval '90 minutes'),
         ('gen-il2', 'scope-il', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, visible_at, payload, created_at, updated_at
) VALUES ('projector_scope-il_gen-il1', 'scope-il', 'gen-il1', 'projector',
          'source_local', 'retrying', 2, now() + interval '65 seconds',
          '{}'::jsonb, now() - interval '2 hours', now() - interval '2 hours'),
         ('projector_scope-il_gen-il2', 'scope-il', 'gen-il2', 'projector',
          'source_local', 'pending', 0, now() - interval '1 hour',
          '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour');
`

// resetMarkGuardClaimRaceOldest puts gen-il1 back to "retrying, marked,
// visible at $1" before each iteration. It stays its own statement because a
// parameterized multi-statement cannot run prepared.
const resetMarkGuardClaimRaceOldest = `
UPDATE fact_work_items
SET status = 'retrying', attempt_count = 2, lease_owner = NULL, claim_until = NULL,
    visible_at = $1, next_attempt_at = $1,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = 'projector_scope-il_gen-il1'
`

// resetMarkGuardClaimRaceNewest puts gen-il2 back to pending before each
// iteration.
const resetMarkGuardClaimRaceNewest = `
UPDATE fact_work_items
SET status = 'pending', attempt_count = 0, lease_owner = NULL, claim_until = NULL,
    visible_at = now() - interval '1 hour', next_attempt_at = NULL,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = 'projector_scope-il_gen-il2'
`

// markGuardClaimRaceIterations is the #7469 ruling's N for retry-claim
// against newer-claim.
const markGuardClaimRaceIterations = 1000

// TestProjectorClaimRetryAgainstNewerInterleave is the #7469 admission-guard
// proof under timing chaos: 1,000 times, racer A (whose clock sees gen-il1's
// marked retry visible) and racer B (whose clock sees it still invisible)
// claim concurrently, simultaneously or with either side delayed by a seeded
// random 0-400us. A must win the retry every time; B must never take gen-il2
// behind it, whether B's snapshot predates A's commit (the guard holds gen-il2
// because gen-il1 waits) or follows it (the in-flight guard holds gen-il2
// because gen-il1 is live). B's statement locks nothing on this shape, so the
// two racers cannot conflict; any error or deadlock is a bug.
func TestProjectorClaimRetryAgainstNewerInterleave(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	control := openClaimDeadlockProofDB(t, dsn, 4)
	if _, err := control.Exec(markGuardClaimRaceSeed); err != nil {
		t.Fatalf("seed race scope: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	retryDB := openSessionOnProofSchema(ctx, t, control, dsn)
	newerDB := openSessionOnProofSchema(ctx, t, control, dsn)
	retryQueue := NewProjectorQueue(SQLDB{DB: retryDB}, "retry-claimer", time.Minute)
	newerQueue := NewProjectorQueue(SQLDB{DB: newerDB}, "newer-claimer", time.Minute)

	deadlocksBefore := proofDatabaseDeadlocks(ctx, t, control)
	errs := map[string]int{}
	aWon, bWon := 0, 0
	rng := rand.New(rand.NewSource(7469))
	for i := 0; i < markGuardClaimRaceIterations; i++ {
		base := time.Now().UTC().Add(time.Minute)
		if _, err := control.ExecContext(ctx, resetMarkGuardClaimRaceOldest, base.Add(5*time.Second)); err != nil {
			t.Fatalf("iteration %d: reset oldest: %v", i, err)
		}
		if _, err := control.ExecContext(ctx, resetMarkGuardClaimRaceNewest); err != nil {
			t.Fatalf("iteration %d: reset newest: %v", i, err)
		}
		retryQueue.Now = func() time.Time { return base.Add(10 * time.Second) }
		newerQueue.Now = func() time.Time { return base }
		mode := i % 3 // 0 simultaneous, 1 retry delayed, 2 newer delayed
		delay := time.Duration(rng.Intn(400)) * time.Microsecond
		start := make(chan struct{})
		var wg sync.WaitGroup
		var aWork, bWork string
		var aOK, bOK bool
		var aErr, bErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if mode == 1 {
				time.Sleep(delay)
			}
			work, ok, err := retryQueue.Claim(ctx)
			aWork, aOK, aErr = work.Generation.GenerationID, ok, err
		}()
		go func() {
			defer wg.Done()
			<-start
			if mode == 2 {
				time.Sleep(delay)
			}
			work, ok, err := newerQueue.Claim(ctx)
			bWork, bOK, bErr = work.Generation.GenerationID, ok, err
		}()
		close(start)
		wg.Wait()

		if aErr != nil {
			errs["retry "+sqlStateLabel(aErr)]++
		}
		if bErr != nil {
			errs["newer "+sqlStateLabel(bErr)]++
		}
		if aOK {
			aWon++
		}
		if bOK {
			bWon++
		}
		if !aOK || aWork != "gen-il1" {
			t.Fatalf("iteration %d: retry-claimer = (%q, %v, %v), want gen-il1",
				i, aWork, aOK, aErr)
		}
		if bOK {
			t.Fatalf("iteration %d: newer-claimer took %q, want nothing behind the marked retry", i, bWork)
		}
		var status, owner, newerStatus string
		var marked bool
		if err := control.QueryRowContext(ctx, `
SELECT w1.status, w1.lease_owner, g1.projection_write_started_at IS NOT NULL,
       (SELECT status FROM fact_work_items WHERE work_item_id = 'projector_scope-il_gen-il2')
FROM fact_work_items AS w1
JOIN scope_generations AS g1 ON g1.generation_id = w1.generation_id
WHERE w1.work_item_id = 'projector_scope-il_gen-il1'`,
		).Scan(&status, &owner, &marked, &newerStatus); err != nil {
			t.Fatalf("iteration %d: read state: %v", i, err)
		}
		if status != "claimed" || owner != "retry-claimer" || !marked || newerStatus != "pending" {
			t.Fatalf("iteration %d: gen-il1 = (%s, %q, marked %v), gen-il2 = %s; want il1 claimed by retry-claimer and il2 pending",
				i, status, owner, marked, newerStatus)
		}
	}
	flushProofSessionStats(ctx, t, retryDB, newerDB)
	deadlocksAfter := proofDatabaseDeadlocks(ctx, t, control)

	t.Logf("interleave iterations=%d retry_won=%d newer_won=%d deadlocks before=%d after=%d delta=%d",
		markGuardClaimRaceIterations, aWon, bWon, deadlocksBefore, deadlocksAfter, deadlocksAfter-deadlocksBefore)
	for _, line := range sortedCounts("error", errs) {
		t.Log(line)
	}
	if bWon != 0 {
		t.Errorf("newer-claimer won %d of %d races, want 0", bWon, markGuardClaimRaceIterations)
	}
	if delta := deadlocksAfter - deadlocksBefore; delta != 0 {
		t.Errorf("pg_stat_database.deadlocks delta = %d, want 0", delta)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected statement errors: %v", errs)
	}
}
