// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestReducerContentionGateSupplyChainImpactAttachmentWaitUnderContentionLive
// is the #11105 contention proof. Six workers claim every domain from one
// pooled database (so claims, fails, and acks interleave on separate
// connections) and drain eight image scopes, each holding a retrying
// attachment row with a short future visible_at and a pending impact row,
// beside eight unrelated scopes that hold only an impact row. The real
// clock drives the queue, so an impact pass that parks can only come back
// through the attachment Ack wake inside the hang guard: the 10-minute
// backstop is far outside it. The invariants:
//
//   - one scope never has two rows claimed or running at once (the conflict
//     fence still holds under the new class), read from fact_work_items at
//     every claim. An in-process counter would also count the benign window
//     where an attachment Ack has committed, released the fence, and is
//     still running its post-commit wake when another worker claims the
//     reopened impact row;
//   - no statement fails, so no 23505 or deadlock surfaces from the wait,
//     the park, or the wake;
//   - every impact row ends succeeded at attempt_count 1, every image scope
//     publishes its attachment-backed finding, and the unrelated scopes drain
//     alongside without waiting;
//   - worker count and claim settings are the queue defaults; nothing is
//     serialized.
//
// The wall clock bounds the test as a hang guard only; it is logged.
func TestReducerContentionGateSupplyChainImpactAttachmentWaitUnderContentionLive(t *testing.T) {
	w := newAttachWait(t)
	const imageScopes, unrelatedScopes, workers = 8, 8, 6
	ctx, cancel := context.WithTimeout(w.ctx, 120*time.Second)
	defer cancel()
	now := time.Now().UTC()
	w.clock = now
	for i := 0; i < imageScopes; i++ {
		scope := fmt.Sprintf("scope:11105:c-image-%02d", i)
		seedAttachWaitScope(w, scope)
		backoff := time.Duration(300+100*i) * time.Millisecond
		w.seedWorkItem(fmt.Sprintf("attach-c%02d", i), scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", now.Add(backoff))
		w.seedWorkItem(fmt.Sprintf("impact-c%02d", i), scope, reducercontract.DomainSupplyChainImpact, "pending", now.Add(-time.Minute))
	}
	for i := 0; i < unrelatedScopes; i++ {
		scope := fmt.Sprintf("scope:11105:c-plain-%02d", i)
		fanOutSeedScope(t, ctx, w.db, scope, "g1")
		w.seedWorkItem(fmt.Sprintf("impact-p%02d", i), scope, reducercontract.DomainSupplyChainImpact, "pending", now.Add(-time.Minute))
	}
	handler := w.handler
	handler.Now = nil // the real clock, like the queue below

	var (
		overlaps  atomic.Int64
		deferrals atomic.Int64
		remaining atomic.Int64
	)
	remaining.Store(imageScopes*2 + unrelatedScopes)
	// enter reads the fence from the database: the claimed row must be the
	// only claimed or running row of its scope.
	enter := func(scope string) error {
		var live int
		if err := w.db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
WHERE scope_id = $1 AND stage = 'reducer' AND status IN ('claimed', 'running')`, scope).Scan(&live); err != nil {
			return fmt.Errorf("read fence for %s: %w", scope, err)
		}
		if live > 1 {
			overlaps.Add(1)
		}
		return nil
	}

	started := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: w.db}, fmt.Sprintf("worker-%d", worker), time.Minute)
		queue.RetryDelay, queue.MaxAttempts = 20*time.Millisecond, 3
		wg.Add(1)
		go func() {
			defer wg.Done()
			for remaining.Load() > 0 && ctx.Err() == nil {
				intent, claimed, err := queue.Claim(ctx)
				if err != nil {
					errs <- fmt.Errorf("claim: %w", err)
					return
				}
				if !claimed {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				runErr := enter(intent.ScopeID)
				if runErr == nil {
					runErr = runContendedIntent(ctx, w, queue, handler, intent, &deferrals, &remaining)
				}
				if runErr != nil {
					errs <- runErr
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(err.Error(), "23505") {
			t.Errorf("unique violation under contention: %v", err)
			continue
		}
		t.Error(err)
	}
	if remaining.Load() != 0 {
		t.Fatalf("%d rows not drained before the hang guard fired: a parked pass missed its wake", remaining.Load())
	}
	if overlaps.Load() != 0 {
		t.Fatalf("%d overlaps: one scope had two rows claimed or running together", overlaps.Load())
	}
	if deferrals.Load() == 0 {
		t.Fatal("no impact pass ran before its attachment: the scenario was not exercised")
	}
	var badImpact, badAttach int
	if err := w.db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE domain = 'supply_chain_impact' AND (status <> 'succeeded' OR attempt_count <> 1)),
       count(*) FILTER (WHERE domain = 'sbom_attestation_attachment' AND status <> 'succeeded')
FROM fact_work_items WHERE scope_id LIKE 'scope:11105:c-%'`).Scan(&badImpact, &badAttach); err != nil {
		t.Fatalf("read terminal state: %v", err)
	}
	if badImpact != 0 || badAttach != 0 {
		t.Fatalf("rows not terminal as expected: impact=%d attachment=%d", badImpact, badAttach)
	}
	for i := 0; i < imageScopes; i++ {
		scope := fmt.Sprintf("scope:11105:c-image-%02d", i)
		if got := w.attachedFindings(scope); got != 1 {
			t.Fatalf("%s attached findings = %d, want 1", scope, got)
		}
	}
	t.Logf("drained %d image + %d unrelated scopes with %d workers in %s; %d non-counting deferrals, 0 overlaps",
		imageScopes, unrelatedScopes, workers, time.Since(started).Round(time.Millisecond), deferrals.Load())
}

// runContendedIntent plays one worker step for one claimed intent: the
// attachment writes and acks (production order), the impact pass runs the
// production handler and acks or fails with its error.
func runContendedIntent(
	ctx context.Context,
	w *attachWait,
	queue postgres.ReducerQueue,
	handler interface {
		Handle(context.Context, reducercontract.Intent) (reducercontract.Result, error)
	},
	intent reducercontract.Intent,
	deferrals, remaining *atomic.Int64,
) error {
	switch intent.Domain {
	case reducercontract.DomainSBOMAttestationAttachment:
		if err := w.tryWriteAttachment(intent.ScopeID, intent); err != nil {
			return fmt.Errorf("attachment write %s: %w", intent.ScopeID, err)
		}
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			return fmt.Errorf("attachment ack %s: %w", intent.ScopeID, err)
		}
		remaining.Add(-1)
	case reducercontract.DomainSupplyChainImpact:
		result, err := handler.Handle(ctx, intent)
		if err != nil {
			if failureClassOf(err) != supplyChainImpactAttachmentNotReadyClass {
				return fmt.Errorf("impact %s: unexpected error %w", intent.ScopeID, err)
			}
			deferrals.Add(1)
			if failErr := queue.Fail(ctx, intent, err); failErr != nil {
				return fmt.Errorf("impact fail %s: %w", intent.ScopeID, failErr)
			}
			return nil
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			return fmt.Errorf("impact ack %s: %w", intent.ScopeID, err)
		}
		remaining.Add(-1)
	default:
		return fmt.Errorf("unexpected domain %s", intent.Domain)
	}
	return nil
}
