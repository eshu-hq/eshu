// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 probe of the wait-free relationship retention batch against the real #10820 repository purge: a purge parked mid-flight (it deleted the generation's evidence rows and waits on a candidate row held by another session) makes the batch skip that generation as lock_busy without waiting and without a lock timeout, leave every row of it whole, and still sweep a disjoint generation, and the purge completes once the holder lets go
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
	return f.startPurgeCtx(f.ctx, p)
}

// startPurgeCtx runs the real purge for the scope in the background under ctx,
// so a test can cancel it while it waits and make it roll back.
func (f *liveFixture) startPurgeCtx(ctx context.Context, p purgeScope) <-chan purgeOutcome {
	done := make(chan purgeOutcome, 1)
	go func() {
		purger := postgres.NewRetirementPurger(f.database, f.database)
		result, err := purger.PurgeRetired(ctx, postgres.PurgeRequest{RepoIDs: []string{p.partition}})
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

// requireNoDeadlock fails when err is a deadlock (40P01), the outcome the
// wait-free batch exists to prevent.
func requireNoDeadlock(t *testing.T, who string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
		t.Fatalf("%s was the deadlock victim: %v", who, err)
	}
}

// sweepOutcome is what one background Sweep returned.
type sweepOutcome struct {
	result relationshipretention.Result
	err    error
}

// parkedSweep is a sweep whose first batch stops after one statement, so a test
// can start a purge while the batch holds exactly the locks it holds at that
// point, and resume it at a moment of the test's choosing.
type parkedSweep struct {
	parked chan struct{}
	resume chan struct{}
	done   chan sweepOutcome
	once   sync.Once
}

// startParkedSweep runs a sweep with deletes on in the background and parks its
// first batch after the named stage until release is called.
func (f *liveFixture) startParkedSweep(stage string) *parkedSweep {
	p := &parkedSweep{parked: make(chan struct{}), resume: make(chan struct{}), done: make(chan sweepOutcome, 1)}
	var first sync.Once
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(at string) error {
		if at != stage {
			return nil
		}
		parking := false
		first.Do(func() { parking = true })
		if !parking {
			return nil
		}
		close(p.parked)
		select {
		case <-p.resume:
			return nil
		case <-time.After(60 * time.Second):
			return errors.New("the probe never resumed the parked batch")
		}
	})
	go func() {
		ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
		defer cancel()
		result, err := relationshipretention.Sweep(ctx, f.database, opts)
		p.done <- sweepOutcome{result, err}
	}()
	return p
}

// awaitParked waits until the first batch has run the stage it parks after.
func (p *parkedSweep) awaitParked(t *testing.T) {
	t.Helper()
	select {
	case <-p.parked:
	case <-time.After(30 * time.Second):
		t.Fatal("the batch never reached the statement it parks after")
	}
}

// release lets the parked batch carry on.
func (p *parkedSweep) release() { p.once.Do(func() { close(p.resume) }) }

// outcome waits for the sweep to finish.
func (p *parkedSweep) outcome(t *testing.T) sweepOutcome {
	t.Helper()
	select {
	case got := <-p.done:
		return got
	case <-time.After(60 * time.Second):
		t.Fatal("the sweep did not finish")
		return sweepOutcome{}
	}
}

// TestSweepYieldsToAPurgeInFlightLive parks the real purge mid-flight: a holder
// owns every candidate row of the orphan generation, so the purge has already
// deleted (and holds the row locks of) the generation's evidence and waits for
// the first candidate row. A batch for that generation locks the generation row,
// finds no evidence or candidate row it can take, and must neither wait nor
// delete anything: it counts the generation as lock_busy (never lock_timeout),
// leaves every row of it, and carries on, so a disjoint generation in the next
// batch is still swept. Releasing the holder lets the purge finish.
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

	release := f.holdCandidates(t, inPurge)
	purge := f.startPurge(p)
	if !f.waitForLockWait(t, "DELETE FROM relationship_candidates WHERE generation_id IN%", 30*time.Second) {
		t.Fatal("the purge never waited on the held candidate rows")
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
		t.Fatalf("Sweep: %v (a sweep that waits on a purge never yields)", err)
	}
	t.Logf("sweep returned after %s with the purge parked", elapsed.Round(time.Millisecond))
	if result.Skipped[relationshipretention.SkipLockBusy] != 1 || result.Skipped[relationshipretention.SkipLockTimeout] != 0 ||
		result.GenerationsSwept != 1 || result.Batches != 1 {
		t.Fatalf("pass = swept %d in %d batches, skipped %v; want the purge's generation skipped as lock_busy (no lock timeout) and the disjoint one swept",
			result.GenerationsSwept, result.Batches, result.Skipped)
	}
	if got := f.of(t, inPurge); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("the purge's generation = %+v while it is parked, want every row left whole {3 2 1 0}", got)
	}
	if got := f.of(t, disjoint); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("the disjoint generation = %+v, want it swept", got)
	}

	release()
	requirePurgeCompleted(t, waitForPurge(t, purge), p)
	if got := f.of(t, inPurge); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("the purge's generation = %+v after the purge, want it gone", got)
	}
}
