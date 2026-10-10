// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn proves the #7469
// sweep never waits on a write-start marker that is still in flight: the
// marker transaction holds gen-mg1's generation row while the claim runs. The
// sweep must skip the held row without waiting, claim nothing (gen-mg1 is
// supersedable in the snapshot but busy, so gen-mg2 is not oldest), and leave
// gen-mg1 alone. Once the holder rolls back, the follow-up claim supersedes
// the unmarked gen-mg1 and claims gen-mg2, proving the skip was lock-based.
func TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	ctx := context.Background()
	seedClaimMaintenanceScopes(t, database, "scope-mb")
	at := time.Now().UTC().Truncate(time.Second)
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mb", "gen-mg1", "pending", "retrying", 2,
		at.Add(-2 * time.Hour), at.Add(-time.Hour), at.Add(-2 * time.Hour),
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-mb", "gen-mg2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(
		`SELECT 1 FROM scope_generations WHERE generation_id = 'gen-mg1' FOR NO KEY UPDATE`,
	); err != nil {
		t.Fatalf("hold gen-mg1 generation row: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	started := time.Now()
	work, ok, err := queue.Claim(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Claim() error = %v, want no wait on the held generation row", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Claim() took %s with the generation row held, want no wait", elapsed)
	}
	if ok {
		t.Fatalf("Claim() = %q with the generation row held, want no claim", work.Generation.GenerationID)
	}
	if status, _, _ := workState(t, database, "scope-mb", "gen-mg1"); status != "retrying" {
		t.Fatalf("gen-mg1 work = %s, want retrying while its generation row is held", status)
	}

	if err := holder.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("release holder: %v", err)
	}
	work, ok, err = queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-mg2" {
		t.Fatalf("Claim() after release = (%q, %v, %v), want gen-mg2", work.Generation.GenerationID, ok, err)
	}
	if status, class, _ := workState(t, database, "scope-mb", "gen-mg1"); status != "superseded" ||
		class != "projector_superseded_by_newer_generation" {
		t.Fatalf("gen-mg1 work = (%s, %q), want superseded once unlocked", status, class)
	}
}

// markGuardRaceScope seeds the #7469 cross-snapshot race: scope-s holds the
// marked gen-mg1 retry (visible at visibleAt) with newer pending gen-mg2, and
// scope-z holds the supersedable gen-z1 beside pending gen-z2, which fires the
// pause trigger and gives the paused claimer its other-scope row.
func markGuardRaceScope(t *testing.T, database *sql.DB, at, visibleAt time.Time) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
	for _, work := range []fenceProofWork{
		{"scope-s", "gen-s1", "pending", "retrying", 2, at.Add(-2 * time.Hour), visibleAt, at.Add(-2 * time.Hour)},
		{"scope-s", "gen-s2", "pending", "pending", 0, at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour)},
		{"scope-z", "gen-z1", "pending", "pending", 0, at.Add(-3 * time.Hour), at.Add(-3 * time.Hour), at.Add(-3 * time.Hour)},
		{"scope-z", "gen-z2", "pending", "pending", 0, at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-30 * time.Minute)},
	} {
		seedFenceProofWork(t, database, work)
	}
	if _, err := database.Exec(
		`UPDATE scope_generations SET projection_write_started_at = $1 WHERE generation_id = 'gen-s1'`,
		at.Add(-90*time.Minute),
	); err != nil {
		t.Fatalf("mark gen-s1: %v", err)
	}
}

// TestProjectorClaimDropsHolderClaimedAfterSnapshot is the #7469 EvalPlanQual
// proof for the admission guard's holder. The paused claimer B snapshots
// gen-s1 as the oldest row of scope-s and offers it to its lock step. Racer A
// then claims gen-s1 and commits, bumping the scope fence. B must drop gen-s1
// at lock time through the work-row and fence rechecks, must not fall through
// to the held gen-s2, and must land on gen-z2 instead, leaving scope-s with
// exactly the one lease A holds.
func TestProjectorClaimDropsHolderClaimedAfterSnapshot(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	markGuardRaceScope(t, database, at, at.Add(-time.Hour))
	paused := openPausedClaimPool(t, database, dsn)

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-b", time.Minute)
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, ok, err}
	}()
	waitForPausedClaimer(t, dsn)

	racer := NewProjectorQueue(SQLDB{DB: database}, "worker-a", time.Minute)
	work, ok, err := racer.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s1" {
		t.Fatalf("racing Claim() = (%q, %v, %v), want gen-s1", work.Generation.GenerationID, ok, err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if got := claimedLeaseCount(t, database, "scope-s"); got != 1 {
		t.Fatalf("scope-s holds %d leases after both claims, want 1 (paused claim took %q)", got, result.generation)
	}
	if !result.ok || result.generation != "gen-z2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-z2 in the other scope", result.generation, result.ok)
	}
	if status, _, owner := workState(t, database, "scope-s", "gen-s1"); status != "claimed" || owner != "worker-a" {
		t.Fatalf("gen-s1 = (%s, %q), want claimed by worker-a", status, owner)
	}
	if status, _, _ := workState(t, database, "scope-s", "gen-s2"); status != "pending" {
		t.Fatalf("gen-s2 = %s, want pending: the paused claimer must not fall through to the held row", status)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	if got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background()); err != nil || got != 0 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = (%d, %v), want 0", got, err)
	}
}

// TestProjectorClaimGuardAgreesWithFenceAcrossClocks replays the #7115 race
// with a marked holder: claimer B snapshots at T, when gen-s1's retry is still
// invisible, and pauses inside its supersede UPDATE. Racer A, with clock
// T+10s, claims the now-visible gen-s1 and commits. B's snapshot still sees
// gen-s1 waiting, so the guard holds gen-s2 there, and the fence A's claim
// bumped drops scope-s at lock time. Both defenders must agree: B lands on
// gen-z2 and scope-s keeps exactly A's lease.
func TestProjectorClaimGuardAgreesWithFenceAcrossClocks(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	markGuardRaceScope(t, database, at, at.Add(5*time.Second))
	paused := openPausedClaimPool(t, database, dsn)

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-b", time.Minute)
		queue.Now = func() time.Time { return at }
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{work.Generation.GenerationID, ok, err}
	}()
	waitForPausedClaimer(t, dsn)

	racer := NewProjectorQueue(SQLDB{DB: database}, "worker-a", time.Minute)
	racer.Now = func() time.Time { return at.Add(10 * time.Second) }
	work, ok, err := racer.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s1" {
		t.Fatalf("racing Claim() = (%q, %v, %v), want gen-s1", work.Generation.GenerationID, ok, err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if got := claimedLeaseCount(t, database, "scope-s"); got != 1 {
		t.Fatalf("scope-s holds %d leases after both claims, want 1 (paused claim took %q)", got, result.generation)
	}
	if !result.ok || result.generation != "gen-z2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-z2 in the other scope", result.generation, result.ok)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	if got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background()); err != nil || got != 0 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = (%d, %v), want 0", got, err)
	}
}

// markGuardSweepRaceSeed is the #7469 marker-vs-sweep fixture: gen-il1 is an
// unmarked retrying row with the newer pending gen-il2 beside it, so the
// claim sweep wants gen-il1 at the same moment the marker race writes it.
const markGuardSweepRaceSeed = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ('scope-il', 'repository', 'git', 'scope-il', 'git',
          'scope-il', now(), now(), 'active');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('gen-il1', 'scope-il', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'pending'),
         ('gen-il2', 'scope-il', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'pending');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, visible_at, payload, created_at, updated_at
) VALUES ('projector_scope-il_gen-il1', 'scope-il', 'gen-il1', 'projector',
          'source_local', 'retrying', 2, now() - interval '1 hour',
          '{}'::jsonb, now() - interval '2 hours', now() - interval '2 hours'),
         ('projector_scope-il_gen-il2', 'scope-il', 'gen-il2', 'projector',
          'source_local', 'pending', 0, now() - interval '1 hour',
          '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour');
`

// resetMarkGuardSweepRaceGenerations puts gen-il1 and gen-il2 back to
// "pending, unmarked" before each iteration.
const resetMarkGuardSweepRaceGenerations = `
UPDATE scope_generations
SET status = 'pending', projection_write_started_at = NULL, superseded_at = NULL
WHERE generation_id IN ('gen-il1', 'gen-il2')
`

// resetMarkGuardSweepRaceOldest puts gen-il1's work row back to retrying with
// the given updated_at before each iteration. The updated_at is forced per
// trial: even trials restore the natural order (gen-il1 oldest), odd trials
// force the drifted order (gen-il1's retry failed after gen-il2 was
// enqueued), so the oldest-ready order cannot resonate with the delay cycle.
// Each reset stays its own statement because a parameterized multi-statement
// cannot run prepared.
const resetMarkGuardSweepRaceOldest = `
UPDATE fact_work_items
SET status = 'retrying', attempt_count = 2, lease_owner = NULL, claim_until = NULL,
    visible_at = now() - interval '1 hour', next_attempt_at = NULL,
    failure_class = NULL, failure_message = NULL, failure_details = NULL,
    updated_at = $1
WHERE work_item_id = 'projector_scope-il_gen-il1'
`

// resetMarkGuardSweepRaceNewest puts gen-il2's work row back to pending with
// a fixed updated_at before each iteration.
const resetMarkGuardSweepRaceNewest = `
UPDATE fact_work_items
SET status = 'pending', attempt_count = 0, lease_owner = NULL, claim_until = NULL,
    visible_at = now() - interval '1 hour', next_attempt_at = NULL,
    failure_class = NULL, failure_message = NULL, failure_details = NULL,
    updated_at = now() - interval '1 hour'
WHERE work_item_id = 'projector_scope-il_gen-il2'
`

// readMarkGuardSweepRaceState is "gen-il1 work status,marker set,gen-il2 work
// status".
const readMarkGuardSweepRaceState = `
SELECT w1.status || ',' || (g1.projection_write_started_at IS NOT NULL)::text || ',' || w2.status
FROM scope_generations AS g1
JOIN fact_work_items AS w1 ON w1.generation_id = g1.generation_id
JOIN fact_work_items AS w2 ON w2.generation_id = 'gen-il2'
WHERE g1.generation_id = 'gen-il1'
`

// markGuardSweepRaceMarker is the racing marker's generation-row write. It
// carries the shipped marker's status fence but not its lease EXISTS: gen-il1
// must stay retrying for the sweep to engage it, and on a retrying row the
// shipped EXISTS could never pass. The lease fence is orthogonal and pinned by
// the write-marker shape tests. The race runs this inside the shipped fence
// protocol (lockProjectorMarkerFenceQuery, then this write, then
// bumpProjectorMarkerFenceQuery), using the shipped consts verbatim so the
// modeled marker cannot drift from the real one.
const markGuardSweepRaceMarker = `
UPDATE scope_generations
SET projection_write_started_at = now()
WHERE generation_id = 'gen-il1'
  AND status IN ('pending', 'failed')
`

// runMarkGuardSweepRaceMarker runs one racing marker attempt against
// scope-il: lock the claim fence row, write the marker, bump the fence, and
// commit. A busy fence defers (rollback, nil error), exactly like the shipped
// MarkProjectionWriteStarted. The generation-row write is the modeled
// markGuardSweepRaceMarker; the fence statements are the shipped consts.
func runMarkGuardSweepRaceMarker(ctx context.Context, t *testing.T, markerDB *sql.DB) error {
	t.Helper()
	tx, err := markerDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var fence int64
	if err := tx.QueryRowContext(ctx, lockProjectorMarkerFenceQuery, "scope-il").Scan(&fence); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, markGuardSweepRaceMarker); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, bumpProjectorMarkerFenceQuery, "scope-il"); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// markGuardSweepRaceIterations is the #7469 ruling's N for marker-vs-sweep.
const markGuardSweepRaceIterations = 1000

// TestProjectorClaimMarkerSweepRace is the #7469 exclusion proof for the
// sweep's lock-time marker truth: a marker commit and the claim sweep race on
// gen-il1 from two sessions, 1,000 times, simultaneously or with either side
// delayed by a seeded random 0-400us. The sweep's generation locks land
// microseconds after its snapshot, before any pausable UPDATE, so this race
// cannot be choreographed deterministically; the assertions hold under every
// alignment. Exactly one side may win each race: a marker that commits must
// leave the retry spared (claimed or still retrying) with gen-il2 pending
// behind it, and a sweep that wins must leave gen-il1 superseded with no
// marker while the claim takes gen-il2. Both winning (gen-il1 superseded AND
// marked) is the double win the marker_spared flag closes. Sparing gen-il1
// while claiming gen-il2 in the same statement is the split decision the
// #7819 fence protocol closes: the marker locks the scope's claim fence row
// before writing, so a marker in flight makes the claim skip the scope, a
// claim in flight makes the marker defer, and a marker that commits between
// the claim's snapshot and its fence lock drops the candidate through the
// #7115 fence recheck. The racing marker models a real attempt: it defers on
// a busy fence and retries nothing, so a deferred trial ends with the sweep
// winning outright.
func TestProjectorClaimMarkerSweepRace(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	control := openClaimDeadlockProofDB(t, dsn, 4)
	if _, err := control.Exec(markGuardSweepRaceSeed); err != nil {
		t.Fatalf("seed race scope: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	markerDB := openSessionOnProofSchema(ctx, t, control, dsn)
	sweepDB := openSessionOnProofSchema(ctx, t, control, dsn)
	sweepQueue := NewProjectorQueue(SQLDB{DB: sweepDB}, "sweep-worker", time.Minute)

	deadlocksBefore := proofDatabaseDeadlocks(ctx, t, control)
	finals := map[string]int{}
	errs := map[string]int{}
	rng := rand.New(rand.NewSource(7469))
	for i := 0; i < markGuardSweepRaceIterations; i++ {
		drifted := i%2 == 1
		oldestUpdated := time.Now().UTC().Add(-2 * time.Hour)
		if drifted {
			oldestUpdated = time.Now().UTC()
		}
		if _, err := control.ExecContext(ctx, resetMarkGuardSweepRaceGenerations); err != nil {
			t.Fatalf("iteration %d: reset generations: %v", i, err)
		}
		if _, err := control.ExecContext(ctx, resetMarkGuardSweepRaceOldest, oldestUpdated); err != nil {
			t.Fatalf("iteration %d: reset oldest: %v", i, err)
		}
		if _, err := control.ExecContext(ctx, resetMarkGuardSweepRaceNewest); err != nil {
			t.Fatalf("iteration %d: reset newest: %v", i, err)
		}
		mode := i % 3 // 0 simultaneous, 1 marker delayed, 2 sweep delayed
		delay := time.Duration(rng.Intn(400)) * time.Microsecond
		start := make(chan struct{})
		var wg sync.WaitGroup
		var markerErr, sweepErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if mode == 1 {
				time.Sleep(delay)
			}
			markerErr = runMarkGuardSweepRaceMarker(ctx, t, markerDB)
		}()
		go func() {
			defer wg.Done()
			<-start
			if mode == 2 {
				time.Sleep(delay)
			}
			_, _, sweepErr = sweepQueue.Claim(ctx)
		}()
		close(start)
		wg.Wait()

		if markerErr != nil {
			errs["marker "+sqlStateLabel(markerErr)]++
		}
		if sweepErr != nil {
			errs["sweep "+sqlStateLabel(sweepErr)]++
		}
		var state string
		if err := control.QueryRowContext(ctx, readMarkGuardSweepRaceState).Scan(&state); err != nil {
			t.Fatalf("iteration %d: read state: %v", i, err)
		}
		order := "natural"
		if drifted {
			order = "drifted"
		}
		finals[order+" "+state]++
	}
	flushProofSessionStats(ctx, t, markerDB, sweepDB)
	deadlocksAfter := proofDatabaseDeadlocks(ctx, t, control)

	t.Logf("interleave iterations=%d deadlocks before=%d after=%d delta=%d",
		markGuardSweepRaceIterations, deadlocksBefore, deadlocksAfter, deadlocksAfter-deadlocksBefore)
	for _, line := range sortedCounts("final", finals) {
		t.Log(line)
	}
	for _, line := range sortedCounts("error", errs) {
		t.Log(line)
	}

	splits := map[string]int{}
	for final, count := range finals {
		order, state, _ := strings.Cut(final, " ")
		parts := strings.Split(state, ",")
		if len(parts) != 3 {
			t.Fatalf("parse final %q: want 3 comma parts", final)
		}
		workStatus, marked, newerStatus := parts[0], parts[1], parts[2]
		// The double win: the sweep retired a generation that set its marker.
		if workStatus == "superseded" && marked == "true" {
			t.Errorf("double win %q seen %d times: the sweep retired a marked generation", final, count)
		}
		// The split decision is ASSERTED zero: the sweep spared the marked
		// retry but the same statement claimed the newer row behind it. It
		// needs a three-way conjunction: the holder unmarked in the
		// snapshot, the marker transaction in flight (and so SKIP-invisible)
		// during the sweep's generation pull, and the newer row sorting
		// oldest by updated_at. #7819 closes it with the fence-bump-on-marker
		// protocol change: the marker locks the scope's claim fence row
		// before the generation row, so the marker commit and a claim's
		// fence lock serialize and a claim whose snapshot predates the
		// marker drops its candidate through the #7115 fence recheck.
		if marked == "true" && newerStatus != "pending" {
			splits[order] += count
		}
		if marked == "false" && workStatus == "retrying" {
			t.Errorf("final %q seen %d times: an unmarked retrying row beside a newer sibling must be swept or marked", final, count)
		}
	}
	t.Logf("split decisions (asserted zero, see comment): natural=%d drifted=%d of %d",
		splits["natural"], splits["drifted"], markGuardSweepRaceIterations)
	if splits["natural"] != 0 || splits["drifted"] != 0 {
		t.Errorf("split decisions natural=%d drifted=%d, want 0 on both parities: "+
			"the sweep spared a marked retry while the same statement claimed the newer row",
			splits["natural"], splits["drifted"])
	}
	if delta := deadlocksAfter - deadlocksBefore; delta != 0 {
		t.Errorf("pg_stat_database.deadlocks delta = %d, want 0", delta)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected statement errors: %v", errs)
	}
}
