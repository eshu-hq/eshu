// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Residual real-Postgres concurrency gate for issue #4124 (R-15 of epic #4102).
//
// The deterministic replay framework (R-13 #4122) replaces the real reducer
// claim path with a deterministic in-memory work source, which by construction
// stops exercising the real `FOR UPDATE SKIP LOCKED` SQL, the wall-clock lease
// expiry, and the per-claim attempt counter. This gate keeps a small,
// nondeterministic, real-Postgres contention check for exactly those
// guarantees — it is the irreducible remainder the golden replay gate does not
// subsume (documented in docs/internal/design/4102-deterministic-replay-framework.md §10).
//
// What this gate asserts (all deterministic or convergent, so it is CI-safe):
//   - no double-claim: distinct work items drained by concurrent workers are
//     each claimed by exactly one worker (real SKIP LOCKED row fencing);
//   - fencing-token monotonicity: attempt_count strictly increases across
//     claim → expiry → reclaim cycles of the same item;
//   - stale-lease reaping under the REAL wall clock: a lease left to expire is
//     reclaimable by another worker, and is NOT claimable while still live;
//   - conflict-key mutual exclusion with a COMMITTED holder: while one item on
//     a (conflict_domain, conflict_key) holds a live lease, concurrent workers
//     cannot claim a sibling on the same key.
//
// What this gate deliberately does NOT assert: conflict-key mutual exclusion
// when two *pending* siblings are claimed by genuinely simultaneous workers
// before either lease commits. That path has a TOCTOU race in the current
// NOT EXISTS + SKIP LOCKED fence (the live-sibling check reads a snapshot that
// may not yet see a concurrent sibling claim), tracked as #4137 (a completion
// of #3558). Asserting it here would make the gate flaky; the
// committed-holder case above is the deterministic guarantee replay can't cover.
//
// The whole gate is skipped unless a DSN is provided, matching the package's
// other real-Postgres proofs.

const contentionGateClaimDomain = conflictProofClaimDomain

// TestReducerContentionGateNoDoubleClaimUnderConcurrency drives genuinely
// concurrent claimers (each on its own Postgres connection) against a pool of
// distinct work items over many rounds and proves the real SKIP LOCKED path
// never hands the same work item to two workers at once.
func TestReducerContentionGateNoDoubleClaimUnderConcurrency(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	owningDB, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)

	const (
		items   = 16
		workers = 6
		rounds  = 15
	)
	now := time.Date(2026, time.June, 28, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, owningDB, "scope-contention", now)
	for i := 0; i < items; i++ {
		insertReducerFairnessWorkItem(t, ctx, owningDB, reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("contention-%03d", i),
			scopeID:    "scope-contention",
			// Distinct conflict keys: this test isolates same-item SKIP LOCKED
			// fencing from the (separately tracked) cross-sibling conflict fence.
			conflictDomain: reducerConflictDomainCodeGraph,
			conflictKey:    fmt.Sprintf("contention-key-%03d", i),
			generationID:   "gen-fair",
			domain:         string(contentionGateClaimDomain),
			updatedAt:      now.Add(time.Duration(i) * time.Millisecond),
		})
	}

	queues := make([]ReducerQueue, workers)
	for i := range queues {
		queues[i] = ReducerQueue{
			db:            SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)},
			LeaseOwner:    fmt.Sprintf("contention-worker-%d", i),
			LeaseDuration: time.Hour, // long lease: anything claimed stays live for the round
			ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
		}
	}

	for round := 0; round < rounds; round++ {
		if _, err := owningDB.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending', lease_owner = NULL, claim_until = NULL, attempt_count = 0
WHERE stage = 'reducer'`); err != nil {
			t.Fatalf("round %d reset: %v", round, err)
		}

		var (
			mu         sync.Mutex
			claimsByID = map[string][]string{} // work_item_id -> owners that claimed it
			start      = make(chan struct{})
			wg         sync.WaitGroup
		)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(q ReducerQueue) {
				defer wg.Done()
				<-start
				for {
					intent, ok, err := q.Claim(ctx)
					if err != nil {
						mu.Lock()
						claimsByID["__error__"] = append(claimsByID["__error__"], err.Error())
						mu.Unlock()
						return
					}
					if !ok {
						return
					}
					mu.Lock()
					claimsByID[intent.IntentID] = append(claimsByID[intent.IntentID], q.LeaseOwner)
					mu.Unlock()
				}
			}(queues[w])
		}
		close(start)
		wg.Wait()

		if errs := claimsByID["__error__"]; len(errs) > 0 {
			t.Fatalf("round %d claim errors: %v", round, errs)
		}
		for id, owners := range claimsByID {
			if len(owners) != 1 {
				t.Fatalf("round %d: work item %q claimed %d times by %v; SKIP LOCKED let concurrent workers double-claim",
					round, id, len(owners), owners)
			}
		}
		if got := len(claimsByID); got != items {
			t.Fatalf("round %d: drained %d distinct items, want %d (each item must be claimed exactly once)", round, got, items)
		}
	}
}

// TestReducerContentionGateFencingTokenStrictlyIncreases proves the per-item
// attempt counter — the value a holder fences stale writes with — strictly
// increases across claim → real-wall-clock expiry → reclaim cycles.
func TestReducerContentionGateFencingTokenStrictlyIncreases(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	db, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.June, 28, 8, 30, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-fence", now)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "fence-item",
		scopeID:        "scope-fence",
		conflictDomain: reducerConflictDomainCodeGraph,
		conflictKey:    "fence-key",
		generationID:   "gen-fair",
		domain:         string(contentionGateClaimDomain),
		updatedAt:      now,
	})

	// Short, real lease so each claim's lease expires under the real wall clock
	// before the next claim. No injected clock here — this is the real-time path.
	const lease = 250 * time.Millisecond
	newQueue := func(owner string) ReducerQueue {
		return ReducerQueue{
			db:            SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)},
			LeaseOwner:    owner,
			LeaseDuration: lease,
			ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
		}
	}

	var tokens []int
	for cycle := 0; cycle < 3; cycle++ {
		q := newQueue(fmt.Sprintf("fence-worker-%d", cycle))
		intent, ok, err := q.Claim(ctx)
		if err != nil {
			t.Fatalf("cycle %d Claim() error = %v", cycle, err)
		}
		if !ok {
			t.Fatalf("cycle %d Claim() ok = false; the lease from cycle %d should have expired and become reclaimable", cycle, cycle-1)
		}
		tokens = append(tokens, intent.AttemptCount)
		// Let this lease lapse under the real wall clock before the next claim.
		time.Sleep(lease + 100*time.Millisecond)
	}

	for i := 1; i < len(tokens); i++ {
		if tokens[i] <= tokens[i-1] {
			t.Fatalf("fencing token did not strictly increase across reclaims: %v", tokens)
		}
	}
}

// TestReducerContentionGateStaleLeaseReapingUnderRealClock proves an
// unfinished lease blocks other workers while live and becomes reclaimable
// only after it expires under the real wall clock (no injected clock).
func TestReducerContentionGateStaleLeaseReapingUnderRealClock(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	db, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.June, 28, 9, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-reap", now)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "reap-item",
		scopeID:        "scope-reap",
		conflictDomain: reducerConflictDomainCodeGraph,
		conflictKey:    "reap-key",
		generationID:   "gen-fair",
		domain:         string(contentionGateClaimDomain),
		updatedAt:      now,
	})

	const lease = 300 * time.Millisecond
	newQueue := func(owner string) ReducerQueue {
		return ReducerQueue{
			db:            SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)},
			LeaseOwner:    owner,
			LeaseDuration: lease,
			ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
		}
	}
	queueA := newQueue("reap-worker-A")
	queueB := newQueue("reap-worker-B")

	if _, ok, err := queueA.Claim(ctx); err != nil {
		t.Fatalf("worker A Claim() error = %v", err)
	} else if !ok {
		t.Fatal("worker A Claim() ok = false, want true")
	}

	// While A's lease is live, B must not claim the item.
	if _, ok, err := queueB.Claim(ctx); err != nil {
		t.Fatalf("worker B early Claim() error = %v", err)
	} else if ok {
		t.Fatal("worker B claimed the item while worker A's lease was still live")
	}

	// Let A's lease lapse under the real wall clock, then B reclaims it.
	time.Sleep(lease + 150*time.Millisecond)

	if _, ok, err := queueB.Claim(ctx); err != nil {
		t.Fatalf("worker B reclaim Claim() error = %v", err)
	} else if !ok {
		t.Fatal("worker B Claim() ok = false after the lease expired; a reaped lease must be reclaimable")
	}
}

// TestReducerContentionGateConflictKeyMutualExclusionCommittedHolder proves
// that while one item on a (conflict_domain, conflict_key) holds a COMMITTED
// live lease, many concurrent workers cannot claim a sibling on the same key.
// The holder is committed before the racers start, so this is deterministic
// (it does not exercise the separately-tracked pending/pending TOCTOU race).
func TestReducerContentionGateConflictKeyMutualExclusionCommittedHolder(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	owningDB, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.June, 28, 9, 30, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, owningDB, "scope-excl", now)

	const sharedKey = "excl-shared-key"
	for i := 0; i < 2; i++ {
		insertReducerFairnessWorkItem(t, ctx, owningDB, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("excl-%03d", i),
			scopeID:        "scope-excl",
			conflictDomain: reducerConflictDomainCodeGraph,
			conflictKey:    sharedKey,
			generationID:   "gen-fair",
			domain:         string(contentionGateClaimDomain),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}

	// Commit one holder on the shared key first.
	holder := ReducerQueue{
		db:            SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)},
		LeaseOwner:    "excl-holder",
		LeaseDuration: time.Hour,
		ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
	}
	if _, ok, err := holder.Claim(ctx); err != nil {
		t.Fatalf("holder Claim() error = %v", err)
	} else if !ok {
		t.Fatal("holder Claim() ok = false, want true")
	}

	const racers = 6
	var (
		mu      sync.Mutex
		claimed []string
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for i := 0; i < racers; i++ {
		q := ReducerQueue{
			db:            SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)},
			LeaseOwner:    fmt.Sprintf("excl-racer-%d", i),
			LeaseDuration: time.Hour,
			ClaimDomains:  []reducer.Domain{contentionGateClaimDomain},
		}
		wg.Add(1)
		go func(q ReducerQueue) {
			defer wg.Done()
			<-start
			intent, ok, err := q.Claim(ctx)
			if err != nil {
				mu.Lock()
				claimed = append(claimed, "error:"+err.Error())
				mu.Unlock()
				return
			}
			if ok {
				mu.Lock()
				claimed = append(claimed, intent.IntentID)
				mu.Unlock()
			}
		}(q)
	}
	close(start)
	wg.Wait()

	if len(claimed) != 0 {
		t.Fatalf("sibling(s) claimed while a committed lease on the same conflict key was live: %v", claimed)
	}
}
