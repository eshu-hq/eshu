// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 deadlock probes of the relationship retention sweep against the real #10820 repository purge, in both arrival orders: a purge parked mid-flight (holding evidence row locks, waiting on a candidate row) makes the batch wait 500 ms and yield with a lock timeout while a disjoint batch still sweeps and the purge completes; a batch parked after classification (holding the generation row) makes the arriving purge wait on it, and the batch's own lock timeout, not the deadlock detector, breaks the cycle so neither side sees a 40P01
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// purgeScope is one repository the real purge will retire.
type purgeScope struct{ partition, scopeID string }

// seedPurgeScope inserts a repository scope with one terminal generation and
// the pending retirement the purge completes, the smallest shape the purge
// accepts.
func (f *liveFixture) seedPurgeScope(t *testing.T, name string) purgeScope {
	t.Helper()
	p := purgeScope{partition: "PARTITION:" + name, scopeID: "purge-" + name}
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $2, 'git_collector', $3, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
		p.scopeID, "key-"+p.scopeID, p.partition)
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'push', NOW(), NOW(), 'superseded', '{}'::jsonb)`, p.scopeID+"-gen-old", p.scopeID)
	f.exec(t, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'd1', 'superseded', NOW(), NOW())`, "work-"+p.scopeID, p.scopeID, p.scopeID+"-gen-old")
	store := postgres.NewRetirementStore(f.database)
	if _, err := store.InsertPending(f.ctx, postgres.RetirementRequest{
		RetirementID: "rr_" + name, RepoID: p.partition, ScopeID: p.scopeID,
		ReasonCode: postgres.RetirementReasonOperatorRetired, ActorClass: "operator", IdempotencyKeyHash: name + "-key",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	return p
}

// purgeOutcome is what one PurgeRetired call returned.
type purgeOutcome struct {
	result postgres.PurgeResult
	err    error
}

// startPurge runs the real purge for the scope in the background.
func (f *liveFixture) startPurge(p purgeScope) <-chan purgeOutcome {
	done := make(chan purgeOutcome, 1)
	go func() {
		purger := postgres.NewRetirementPurger(f.database, f.database)
		result, err := purger.PurgeRetired(f.ctx, postgres.PurgeRequest{RepoIDs: []string{p.partition}})
		done <- purgeOutcome{result: result, err: err}
	}()
	return done
}

// waitForPurge waits for the purge's outcome.
func waitForPurge(t *testing.T, done <-chan purgeOutcome) purgeOutcome {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(60 * time.Second):
		t.Fatal("the purge did not finish")
		return purgeOutcome{}
	}
}

// requirePurgeCompleted fails unless the purge completed exactly the scope's
// repository without an error (a deadlock victim would surface here as 40P01).
func requirePurgeCompleted(t *testing.T, got purgeOutcome, p purgeScope) {
	t.Helper()
	requireNoDeadlock(t, "the purge", got.err)
	if got.err != nil {
		t.Fatalf("PurgeRetired: %v", got.err)
	}
	if len(got.result.Completed) != 1 || got.result.Completed[0] != p.partition || len(got.result.Blocked) != 0 {
		t.Fatalf("purge result = %+v, want %s completed and nothing blocked", got.result, p.partition)
	}
}

// requireNoDeadlock fails when err is a deadlock (40P01), the outcome the lock
// timeout exists to prevent.
func requireNoDeadlock(t *testing.T, who string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
		t.Fatalf("%s was the deadlock victim: %v", who, err)
	}
}

// TestSweepYieldsToAPurgeInFlightLive parks the real purge mid-flight: a holder
// owns one candidate row of the orphan generation, so the purge has already
// deleted (and holds the row locks of) the generation's evidence and waits for
// the candidate row. A batch for that generation locks the generation row and
// waits on the purge's evidence rows; it must give up after its lock timeout,
// count the generation as lock_timeout and carry on, so a disjoint generation
// in the next batch is still swept. Releasing the holder lets the purge finish.
func TestSweepYieldsToAPurgeInFlightLive(t *testing.T) {
	f := openLiveFixture(t)
	p := f.seedPurgeScope(t, "pa")
	f.scope(t, "scope-other-pa")
	const inPurge, disjoint = "gen-pa-1", "gen-pa-2"
	f.generation(t, p.scopeID, inPurge, "superseded") // no scope generation row: an orphan the purge also owns
	f.evidence(t, inPurge, 3)
	f.candidates(t, inPurge, 2)
	f.generation(t, "scope-other-pa", disjoint, "superseded")
	f.evidence(t, disjoint, 3)
	f.candidates(t, disjoint, 2)

	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	var held string
	if err := holder.QueryRowContext(f.ctx, `SELECT candidate_id FROM relationship_candidates
WHERE generation_id = $1 ORDER BY candidate_id LIMIT 1 FOR UPDATE`, inPurge).Scan(&held); err != nil {
		t.Fatalf("hold a candidate row: %v", err)
	}

	purge := f.startPurge(p)
	if !f.waitForLockWait(t, "DELETE FROM relationship_candidates WHERE generation_id IN%", 30*time.Second) {
		t.Fatal("the purge never waited on the held candidate row")
	}

	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	started := time.Now()
	result, err := relationshipretention.Sweep(ctx, f.database, relationshipretention.Options{
		Deletes: true, RowCap: 1 << 40, BatchGenerations: 1,
	})
	elapsed := time.Since(started)
	requireNoDeadlock(t, "the sweep", err)
	if err != nil {
		t.Fatalf("Sweep: %v (a sweep that waits on a purge without a bound never yields)", err)
	}
	t.Logf("sweep returned after %s with the purge parked", elapsed.Round(time.Millisecond))
	if result.Skipped[relationshipretention.SkipLockTimeout] != 1 || result.GenerationsSwept != 1 || result.Batches != 1 {
		t.Fatalf("pass = swept %d in %d batches, skipped %v; want the purge's generation skipped on a lock timeout and the disjoint one swept",
			result.GenerationsSwept, result.Batches, result.Skipped)
	}
	if got := f.of(t, inPurge); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("the purge's generation = %+v while it is parked, want the timed-out batch to have rolled back whole {3 2 1 0}", got)
	}
	if got := f.of(t, disjoint); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("the disjoint generation = %+v, want it swept", got)
	}

	_ = holder.Rollback()
	requirePurgeCompleted(t, waitForPurge(t, purge), p)
	if got := f.of(t, inPurge); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("the purge's generation = %+v after the purge, want it gone", got)
	}
}

// TestPurgeArrivingMidBatchDoesNotDeadlockLive parks the sweep after its
// classification, so the batch holds the orphan generation's row, then starts
// the real purge. The purge deletes the evidence the batch has not reached yet
// and then waits on the generation row; resuming the batch makes it wait on the
// evidence rows the purge holds: a cycle. The batch's 500 ms lock timeout, well
// under the 1 s deadlock timeout, must break it: the batch ends with a lock
// timeout, the purge completes, and neither side is a deadlock victim.
func TestPurgeArrivingMidBatchDoesNotDeadlockLive(t *testing.T) {
	f := openLiveFixture(t)
	p := f.seedPurgeScope(t, "pb")
	const generation = "gen-pb"
	f.generation(t, p.scopeID, generation, "superseded")
	f.evidence(t, generation, 3)
	f.candidates(t, generation, 2)

	parked, purgeStarted := make(chan struct{}), make(chan struct{})
	var once sync.Once
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(stage string) error {
		if stage != relationshipretention.StageClassify {
			return nil
		}
		once.Do(func() { close(parked) })
		select {
		case <-purgeStarted:
		case <-time.After(60 * time.Second):
			return errors.New("the probe never started the purge")
		}
		// Resume the moment the purge queues behind the generation row the batch
		// holds, so the batch's wait begins within its lock timeout of the
		// purge's and the lock timeout, not the purge's deadlock check, ends it.
		waiting, err := f.lockWaitUntil("DELETE FROM relationship_generations WHERE scope%", 30*time.Second)
		if err != nil {
			return err
		}
		if !waiting {
			return errors.New("the purge never waited on the generation row the batch holds")
		}
		return nil
	})
	type sweepOutcome struct {
		result relationshipretention.Result
		err    error
	}
	sweepDone := make(chan sweepOutcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
		defer cancel()
		result, err := relationshipretention.Sweep(ctx, f.database, opts)
		sweepDone <- sweepOutcome{result, err}
	}()
	select {
	case <-parked:
	case <-time.After(30 * time.Second):
		t.Fatal("the batch never reached its classification")
	}

	purge := f.startPurge(p)
	close(purgeStarted)

	var sweep sweepOutcome
	select {
	case sweep = <-sweepDone:
	case <-time.After(60 * time.Second):
		t.Fatal("the sweep did not finish")
	}
	purged := waitForPurge(t, purge)
	// Either side may be the deadlock victim when the batch's lock timeout does
	// not end the cycle first, so check both before anything else.
	requireNoDeadlock(t, "the sweep", sweep.err)
	requireNoDeadlock(t, "the purge", purged.err)
	if sweep.err != nil {
		t.Fatalf("Sweep: %v", sweep.err)
	}
	if sweep.result.Skipped[relationshipretention.SkipLockTimeout] != 1 || sweep.result.GenerationsSwept != 0 {
		t.Fatalf("pass = swept %d, skipped %v; want the batch skipped on its lock timeout",
			sweep.result.GenerationsSwept, sweep.result.Skipped)
	}
	requirePurgeCompleted(t, purged, p)
	if got := f.of(t, generation); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("generation after the purge = %+v, want it gone", got)
	}
}
