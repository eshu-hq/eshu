// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11104 backward-evidence wake contention proof: concurrent fan-in publishers (in-tx exact wake plus post-commit re-wake), reducer Claim/Fail/Ack workers on the gated deployment_mapping rows, a sibling ClaimBatch/AckBatch worker, the waiting audit, and the activation-obligation Finalize on one scope generation finish with zero 40P01 and no waiter left parked once the phase exists; run under -race; disposable database, about 15 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

const (
	contentionWaiters  = 8
	contentionSiblings = 8
	contentionDeadline = 90 * time.Second
)

// contentionQueue is a real ReducerQueue on the host clock (no shared clock
// pointer, so the workers stay race-free).
func contentionQueue(base *sql.DB, owner string, domains ...reducercontract.Domain) ReducerQueue {
	return ReducerQueue{
		database: SQLDB{DB: base}, LeaseOwner: owner, LeaseDuration: 5 * time.Minute,
		RetryDelay: 30 * time.Second, MaxRetryDelay: time.Hour, JitterFraction: 0.1, MaxAttempts: 3,
		ClaimDomains: domains, Now: func() time.Time { return time.Now().UTC() },
	}
}

// contentionParks and contentionAcks count gated Fails and passing Acks
// across the workers, logged so a run shows the overlap it exercised.
var contentionParks, contentionAcks atomic.Int64

// contentionErrors collects actor errors; 40P01 is counted separately.
type contentionErrors struct {
	mu        sync.Mutex
	errs      []string
	deadlocks int
}

func (c *contentionErrors) add(actor string, err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.Contains(err.Error(), "40P01") || strings.Contains(err.Error(), "deadlock detected") {
		c.deadlocks++
	}
	c.errs = append(c.errs, actor+": "+err.Error())
}

// TestBackwardEvidenceWakeContentionLive is proof 7. Lock order on the
// conflict domain (one scope generation): the fan-in transaction takes the
// repository advisory locks (sorted), then the phase and memo rows, and its
// in-tx wake locks reducer_waiting_items in work_item_id COLLATE "C" order
// before the fact_work_items rows it reopens; the post-commit re-wake, the
// Fail park-time wake, and the audit take the same waiting-then-queue order
// in their own statements; Claim and AckBatch lock fact_work_items with SKIP
// LOCKED or by their claimed ids; activation Finalize locks the scope row,
// then the obligation row, then wakes fact_work_items with SKIP LOCKED. No
// path takes a waiting row after holding a reopened queue row, so no cycle
// can form; Postgres would surface one as 40P01.
func TestBackwardEvidenceWakeContentionLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	contentionParks.Store(0)
	contentionAcks.Store(0)
	const scopeID, genID = "git-repository-scope:acme/be-contend", "be-gen-contend"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-c-dm-0", "", time.Now().UTC())
	for i := 1; i < contentionWaiters; i++ {
		addOwnGateItem(t, ctx, base, fmt.Sprintf("be-c-dm-%d", i), scopeID, genID,
			reducer.DomainDeploymentMapping, time.Now().UTC().Add(-time.Minute))
	}
	for i := 0; i < contentionSiblings; i++ {
		addOwnGateItem(t, ctx, base, fmt.Sprintf("be-c-sib-%d", i), scopeID, genID,
			reducer.DomainCodeImportRepoEdge, time.Now().UTC().Add(-time.Minute))
	}
	if _, err := base.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ('be-c-repo', $1, $2, 'repository', 'be-c-repo', 'git', 'be-c-repo', now(), now(), '{}'::jsonb)`,
		scopeID, genID); err != nil {
		t.Fatalf("seed repository fact: %v", err)
	}
	if _, err := base.ExecContext(ctx, `
INSERT INTO activation_obligations (generation_id, scope_id, work_item_id) VALUES ($1, $2, 'be-c-projector')`,
		genID, scopeID); err != nil {
		t.Fatalf("seed activation obligation: %v", err)
	}
	deadlocksBefore := databaseDeadlocks(t, ctx, base)

	runCtx, cancel := context.WithTimeout(ctx, contentionDeadline)
	defer cancel()
	errs := &contentionErrors{}
	start := make(chan struct{})
	var wg sync.WaitGroup
	actor := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs.add(name, fn())
		}()
	}

	for p := 0; p < 2; p++ {
		fanIn := backwardEvidenceFanIn(base)
		actor(fmt.Sprintf("publisher-%d", p), func() error {
			// Publish only after a waiter parked, so every run exercises
			// park-then-publish and the later rounds overlap live parks.
			for wait := time.Now(); contentionParks.Load() == 0 && time.Since(wait) < 5*time.Second; {
				time.Sleep(time.Millisecond)
			}
			for round := 0; round < 6; round++ {
				time.Sleep(time.Duration(round*7) * time.Millisecond)
				if _, err := fanIn.PublishDeferredBackfillPartitions(runCtx,
					map[backfill.ScopeGenerationPartition][]string{
						{ScopeID: scopeID, GenerationID: genID}: {"repository:" + scopeID},
					}, nil, "", 1, nil); err != nil {
					return err
				}
			}
			return nil
		})
	}
	for w := 0; w < 3; w++ {
		queue := contentionQueue(base, fmt.Sprintf("be-c-worker-%d", w), reducer.DomainDeploymentMapping)
		actor(fmt.Sprintf("worker-%d", w), func() error {
			return runGatedWorker(runCtx, base, queue)
		})
	}
	siblings := contentionQueue(base, "be-c-batch", reducer.DomainCodeImportRepoEdge)
	actor("ackbatch", func() error {
		for runCtx.Err() == nil {
			intents, err := siblings.ClaimBatch(runCtx, 3)
			if err != nil {
				return err
			}
			if len(intents) == 0 {
				return nil
			}
			results := make([]reducercontract.Result, len(intents))
			if err := siblings.AckBatch(runCtx, intents, results); err != nil {
				return err
			}
		}
		return nil
	})
	actor("audit", func() error {
		for runCtx.Err() == nil && !allWaitersSucceeded(runCtx, base) {
			if _, _, _, err := AuditWaitingItems(runCtx, SQLDB{DB: base}, nil, time.Now().UTC(),
				DefaultWaitingAuditInterval, DefaultWaitingAuditBatchSize); err != nil {
				return err
			}
			time.Sleep(3 * time.Millisecond)
		}
		return nil
	})
	obligations := activation.NewStore(SQLDB{DB: base})
	actor("finalize", func() error {
		for runCtx.Err() == nil && !allWaitersSucceeded(runCtx, base) {
			work, err := obligations.Claim(runCtx, "be-c-finalizer", time.Minute)
			if err != nil {
				return err
			}
			if work == nil {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			result, err := obligations.Finalize(runCtx, *work)
			if err != nil && !errors.Is(err, activation.ErrLeaseLost) {
				return err
			}
			if result.Outcome == activation.OutcomeCompleted {
				return nil
			}
		}
		return nil
	})

	close(start)
	wg.Wait()
	t.Logf("gated parks=%d passing acks=%d", contentionParks.Load(), contentionAcks.Load())
	if contentionParks.Load() == 0 {
		t.Fatal("no gated park: the run exercised no park/publish overlap")
	}

	if errs.deadlocks != 0 {
		t.Fatalf("40P01 deadlocks = %d, want 0: %v", errs.deadlocks, errs.errs)
	}
	if len(errs.errs) != 0 {
		t.Fatalf("actor errors: %v", errs.errs)
	}
	if delta := databaseDeadlocks(t, ctx, base) - deadlocksBefore; delta != 0 {
		t.Fatalf("pg_stat_database deadlocks advanced by %d, want 0", delta)
	}
	assertNoWaiterParkedWithPhase(t, ctx, base, scopeID, genID)
}

// runGatedWorker claims deployment_mapping rows, runs the real gate, and
// Fails on the deferral or Acks on a pass, until every waiter succeeded.
func runGatedWorker(ctx context.Context, base *sql.DB, queue ReducerQueue) error {
	handler := backwardEvidenceHandler(base)
	for ctx.Err() == nil {
		if allWaitersSucceeded(ctx, base) {
			return nil
		}
		intent, ok, err := queue.Claim(ctx)
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if !ok {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		_, gateErr := handler.Resolve(ctx, intent.ScopeID, intent.GenerationID)
		var notReady crossrepo.BackwardEvidenceNotReadyError
		switch {
		case errors.As(gateErr, &notReady):
			if err := queue.Fail(ctx, intent, gateErr); err != nil {
				return fmt.Errorf("fail %s: %w", intent.IntentID, err)
			}
			contentionParks.Add(1)
		case gateErr != nil:
			return fmt.Errorf("handler %s: %w", intent.IntentID, gateErr)
		default:
			if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
				return fmt.Errorf("ack %s: %w", intent.IntentID, err)
			}
			contentionAcks.Add(1)
		}
	}
	return fmt.Errorf("deadline: waiters never converged (a parked row stranded on the backstop?)")
}

// allWaitersSucceeded reports whether every deployment_mapping waiter acked.
func allWaitersSucceeded(ctx context.Context, base *sql.DB) bool {
	var open int
	if err := base.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE domain = 'deployment_mapping' AND work_item_id LIKE 'be-c-dm-%' AND status <> 'succeeded'`).Scan(&open); err != nil {
		return false
	}
	return open == 0
}

// assertNoWaiterParkedWithPhase checks the end state without running a
// final audit (which would mask a missed wake): the phase exists, and no
// backward-evidence wait row or backstop-parked waiter remains.
func assertNoWaiterParkedWithPhase(t *testing.T, ctx context.Context, base *sql.DB, scopeID, genID string) {
	t.Helper()
	var phase bool
	if err := base.QueryRowContext(ctx, exactPhaseWaitSatisfiedQuery, scopeID, genID,
		"cross_repo_evidence", "backward_evidence_committed").Scan(&phase); err != nil || !phase {
		t.Fatalf("phase present = %v (err %v), want true", phase, err)
	}
	var parked, waits int
	if err := base.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE domain = 'deployment_mapping' AND status <> 'succeeded'`).Scan(&parked); err != nil {
		t.Fatalf("count open waiters: %v", err)
	}
	if err := base.QueryRowContext(ctx, `
SELECT count(*) FROM reducer_waiting_items WHERE failure_class = $1`,
		crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass).Scan(&waits); err != nil {
		t.Fatalf("count wait rows: %v", err)
	}
	if parked != 0 || waits != 0 {
		t.Fatalf("with the phase present: %d waiters not succeeded, %d wait rows, want 0 and 0", parked, waits)
	}
}

// databaseDeadlocks reads the proof database's deadlock counter after a
// stats flush.
func databaseDeadlocks(t *testing.T, ctx context.Context, base *sql.DB) int64 {
	t.Helper()
	time.Sleep(1500 * time.Millisecond)
	if _, err := base.ExecContext(ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
		t.Fatalf("clear stats snapshot: %v", err)
	}
	var deadlocks int64
	if err := base.QueryRowContext(ctx, `
SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(&deadlocks); err != nil {
		t.Fatalf("read deadlock counter: %v", err)
	}
	return deadlocks
}
