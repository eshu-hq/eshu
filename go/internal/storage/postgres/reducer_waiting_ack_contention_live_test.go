// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11228 contention proof: concurrent projector Acks (the activating Ack and a stale-owner duplicate), reducer Fail parks inside and across the Ack's open window, reducer and projector claims, and the waiting audit on one scope generation end with zero 40P01 and no parked waiter once the Ack committed, before any audit pass after the commit; run under -race; disposable database per round; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ackContentionWaiters is the number of reducer waiters per round.
const ackContentionWaiters = 8

// ackContentionSnapshot copies the recorded actor errors and the 40P01
// count of the shared contentionErrors collector.
func ackContentionSnapshot(c *contentionErrors) ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.errs...), c.deadlocks
}

// ackContentionLoop runs step on its own goroutine until stop closes, recording errors.
// A short pause between steps keeps the loop contending without spinning.
func ackContentionLoop(wg *sync.WaitGroup, stop <-chan struct{}, errs *contentionErrors, source string, step func() error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			errs.add(source, step())
			time.Sleep(2 * time.Millisecond)
		}
	}()
}

// TestWaitingAckParkContentionLive is ruling decision 3's contention proof
// for #11228. Each round holds the activating Ack after its in-tx wakes and
// parks waiters inside that window while a stale-owner duplicate Ack, a
// reducer claim loop, a projector claim loop, and the waiting audit run
// concurrently on the same scope generation.
//
// Round "seeded" parks every waiter inside the window and stops the audit
// before the release, so only the post-commit re-wake can reopen them: with
// the re-wake removed they stay parked (the RED mutant). No waiter parks
// after the commit there, because such a park's re-check sees the
// activation and its own wake reopens every waiter on the shared generation
// key, which would hide the mutant.
//
// Round "audit_throughout" parks half the waiters inside the window and
// half while the hold releases and the Ack commits, and keeps the audit
// running across the commit, racing the re-wake and the parks' own wakes on
// the same wait rows.
//
// Both rounds require zero 40P01 (actor errors and pg_stat_database), no
// parked waiter once the Ack committed, and an explicit audit pass
// afterwards that reopens nobody.
func TestWaitingAckParkContentionLive(t *testing.T) {
	ctx := context.Background()
	for _, auditThroughout := range []bool{false, true} {
		name := "seeded"
		if auditThroughout {
			name = "audit_throughout"
		}
		t.Run(name, func(t *testing.T) {
			runAckParkContentionRound(t, ctx, "ack-contend-"+name, auditThroughout)
		})
	}
}

func runAckParkContentionRound(t *testing.T, ctx context.Context, scopeID string, auditThroughout bool) {
	f := newAckParkRaceFixture(t, ctx, scopeID)
	domain := reducer.DomainDeployableUnitCorrelation
	waiters := []reducercontract.Intent{f.waiter}
	for i := 1; i < ackContentionWaiters; i++ {
		id := fmt.Sprintf("%s-waiter-%d", scopeID, i)
		addOwnGateItem(t, ctx, f.base, id, scopeID, f.genID, domain, f.clock)
		intent, ok, err := f.waiterQ.Claim(ctx)
		if err != nil || !ok || intent.IntentID != id {
			t.Fatalf("claim waiter %s: got %q ok=%v err=%v", id, intent.IntentID, ok, err)
		}
		waiters = append(waiters, intent)
	}
	deadlocksBefore := databaseDeadlocks(t, ctx, f.base)
	errs := &contentionErrors{}
	// Every pool the round uses is opened and pinged before the hold, so
	// connection setup under a loaded host cannot spend the blocked Ack's
	// lock_timeout inside the window.
	parkQueues := make(map[string]ReducerQueue, len(waiters))
	for _, intent := range waiters {
		parkQueues[intent.IntentID] = newOwnGateQueue(pingedFairnessDB(t, ctx, f.targetDSN), &f.clock, domain)
	}
	claimQ := newOwnGateQueue(pingedFairnessDB(t, ctx, f.targetDSN), &f.clock, domain)
	projectorDB := pingedFairnessDB(t, ctx, f.targetDSN)
	auditDB := SQLDB{DB: pingedFairnessDB(t, ctx, f.targetDSN)}
	duplicateDB := pingedFairnessDB(t, ctx, f.targetDSN)
	park := func(wg *sync.WaitGroup, intent reducercontract.Intent) {
		queue := parkQueues[intent.IntentID]
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs.add("park "+intent.IntentID, queue.Fail(ctx, intent, notYetActive(scopeID, f.genID)))
		}()
	}

	blockerPID, release := holdAckAfterGenerationWake(t, ctx, f)
	ackDone := make(chan error, 1)
	go func() { ackDone <- f.ack(ctx, openReducerFairnessClaimerDB(t, ctx, f.targetDSN), nil) }()
	waitBlockedBy(t, ctx, f.base, blockerPID, ackDone)

	// Loops that run for the whole round.
	stopLoops := make(chan struct{})
	var loops sync.WaitGroup
	var reducerClaims atomic.Int64
	ackContentionLoop(&loops, stopLoops, errs, "reducer claim", func() error {
		_, ok, err := claimQ.Claim(ctx)
		if ok {
			reducerClaims.Add(1)
		}
		return err
	})
	projectorQ := NewProjectorQueue(SQLDB{DB: projectorDB},
		"ack-contend-projector", 10*time.Minute)
	frozen := f.clock
	projectorQ.Now = func() time.Time { return frozen }
	ackContentionLoop(&loops, stopLoops, errs, "projector claim", func() error {
		_, _, err := projectorQ.Claim(ctx)
		return err
	})
	stopAudit := make(chan struct{})
	var audit sync.WaitGroup
	ackContentionLoop(&audit, stopAudit, errs, "audit", func() error {
		_, _, _, err := AuditWaitingItems(ctx, auditDB, nil, frozen, DefaultWaitingAuditInterval, 100)
		return err
	})

	// A stale-owner duplicate Ack of the same generation, concurrent with
	// the held one: it must be refused or time out on the scope lock, never
	// deadlock.
	duplicateDone := make(chan error, 1)
	go func() {
		duplicate := NewProjectorQueue(SQLDB{DB: duplicateDB},
			"ack-contend-stale-owner", 10*time.Minute)
		duplicate.Now = func() time.Time { return frozen }
		duplicate.AckScopeLockTimeout = maxProjectorAckLockTimeout
		duplicateDone <- duplicate.Ack(ctx, projector.ScopeGenerationWork{
			Scope: scope.IngestionScope{
				ScopeID: scopeID, CollectorKind: scope.CollectorKind(ackParkRaceCollectorKind),
			},
			Generation:   scope.ScopeGeneration{GenerationID: f.genID},
			AttemptCount: 1,
		}, runtime.Result{})
	}()

	// These waiters park inside the window: the in-tx wakes already ran and
	// the activation is uncommitted.
	split := ackContentionWaiters
	if auditThroughout {
		split = ackContentionWaiters / 2
	}
	var inWindow sync.WaitGroup
	for _, intent := range waiters[:split] {
		park(&inWindow, intent)
	}
	inWindow.Wait()
	if !auditThroughout {
		close(stopAudit)
		audit.Wait()
	}

	// In audit_throughout the other half park while the hold releases and
	// the Ack commits.
	var acrossCommit sync.WaitGroup
	for _, intent := range waiters[split:] {
		park(&acrossCommit, intent)
	}
	release()
	select {
	case err := <-ackDone:
		if err != nil {
			t.Fatalf("activating Ack: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("activating Ack never committed after the hold released")
	}
	acrossCommit.Wait()
	if auditThroughout {
		close(stopAudit)
		audit.Wait()
	}
	close(stopLoops)
	loops.Wait()
	// The duplicate is refused or times out on the scope lock; only a
	// deadlock is a failure.
	if err := <-duplicateDone; err != nil {
		errs.add("duplicate ack (expected refusal) ", err)
		t.Logf("duplicate Ack: %v", err)
	}
	msgs, deadlocks := ackContentionSnapshot(errs)
	if deadlocks != 0 {
		t.Errorf("%d 40P01 under contention: %q", deadlocks, msgs)
	}
	for _, msg := range msgs {
		if !strings.HasPrefix(msg, "duplicate ack (expected refusal)") {
			t.Errorf("concurrent step failed: %s", msg)
		}
	}
	if got := f.generationStatus(t, ctx); got != "active" {
		t.Fatalf("generation status = %q, want active", got)
	}
	// No waiter may stay parked once the Ack committed. In the seeded round
	// no audit pass ran after the commit, so only the in-tx wake, the park
	// re-check, or the post-commit re-wake can have reopened them.
	for _, intent := range waiters {
		got := readWaitingRow(t, ctx, f.base, intent.IntentID)
		if got.waitRows != 0 {
			t.Errorf("waiter %s still parked after the Ack committed (status %q, visible_at %v): lost wake (#11228)",
				intent.IntentID, got.status, got.visibleAt.Time)
			continue
		}
		claimable := got.status == "retrying" && got.visibleAt.Valid && !got.visibleAt.Time.After(f.clock)
		if got.status != "claimed" && !claimable {
			t.Errorf("waiter %s = status %q visible_at %v, want claimed or retrying and claimable at %v",
				intent.IntentID, got.status, got.visibleAt.Time, f.clock)
		}
		if got.attempts != 1 {
			t.Errorf("waiter %s attempt_count = %d, want 1 (the wait never counts)", intent.IntentID, got.attempts)
		}
	}
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: f.base}, nil, f.clock, DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("final audit: %v", err)
	}
	if reopened != 0 {
		t.Errorf("final audit reopened %d waits, want 0: the wakes, not the audit, must reopen them", reopened)
	}
	requireNoNewDeadlocks(t, ctx, f.base, deadlocksBefore)
	t.Logf("round %s: %d reducer claims of woken waiters", scopeID, reducerClaims.Load())
}

// requireNoNewDeadlocks fails when pg_stat_database counted a deadlock
// during the round, including one a caller retried internally (the
// projector Claim retries 40P01). databaseDeadlocks waits out the stats
// flush interval before it reads.
func requireNoNewDeadlocks(t *testing.T, ctx context.Context, db *sql.DB, before int64) {
	t.Helper()
	if got := databaseDeadlocks(t, ctx, db); got != before {
		t.Fatalf("pg_stat_database.deadlocks moved %d -> %d during the round", before, got)
	}
}

// pingedFairnessDB opens a one-connection pool and connects it, so the first
// statement the test sends does not pay for connection setup.
func pingedFairnessDB(t *testing.T, ctx context.Context, targetDSN string) *sql.DB {
	t.Helper()
	db := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres pool: %v", err)
	}
	return db
}
