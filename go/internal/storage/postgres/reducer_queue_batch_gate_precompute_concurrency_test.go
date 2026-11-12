// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestClaimBatchGatePrecomputeStorm is the C-3 storm row for #10565 step 1:
// 8 concurrent ClaimBatch claimers draining one backlog must never hold two
// live leases on one conflict key and must never deliver one work item
// twice. It runs the shipped batch claim (with the precomputed gates), so a
// gate rewrite that broke the representative fence or the locked-CTE
// EvalPlanQual recheck would surface here as a duplicate lease or id.
func TestClaimBatchGatePrecomputeStorm(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the gate-precompute storm proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGateScope(t, ctx, db, "scope-gate-storm", "gen-fair", now)

	const rows = 64
	for i := 0; i < rows; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("storm-%03d", i), scopeID: "scope-gate-storm", generationID: "gen-fair",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("storm-key-%03d", i),
			updatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	const claimers = 8
	claimAt := now.Add(2 * time.Hour)
	var (
		mu      sync.Mutex
		claimed = map[string]int{}
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := ReducerQueue{
				database:      SQLDB{DB: claimerDB},
				LeaseOwner:    fmt.Sprintf("storm-claimer-%d", i),
				LeaseDuration: time.Hour,
				Now:           func() time.Time { return claimAt },
			}
			<-start
			// More rounds than the backlog needs: late rounds race on an
			// almost-drained queue, which is exactly the interleaving the
			// fence must survive.
			for r := 0; r < 20; r++ {
				intents, err := q.ClaimBatch(ctx, 8)
				if err != nil {
					t.Errorf("ClaimBatch() error = %v", err)
					return
				}
				mu.Lock()
				for _, in := range intents {
					claimed[in.IntentID]++
				}
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(claimed) != rows {
		t.Fatalf("storm claimed %d distinct items, want %d", len(claimed), rows)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("work item %q delivered %d times, want exactly once", id, n)
		}
	}
	var dupKeys int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM (
    SELECT conflict_domain, COALESCE(conflict_key, scope_id)
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY conflict_domain, COALESCE(conflict_key, scope_id)
    HAVING count(*) > 1
) dup`, claimAt).Scan(&dupKeys); err != nil {
		t.Fatalf("count duplicate live leases: %v", err)
	}
	if dupKeys != 0 {
		t.Fatalf("%d conflict keys hold more than one live lease after the storm", dupKeys)
	}
}

// TestClaimBatchGatePrecomputeDuplicateDelivery is the C-3 duplicate-delivery
// row: claim, ack to success, and a reopen before the next ack must return
// the row to pending exactly once — the next claim admits it once, and while
// its new lease is live no further claim sees it.
func TestClaimBatchGatePrecomputeDuplicateDelivery(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the gate-precompute duplicate-delivery proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGateScope(t, ctx, db, "scope-gate-dup", "gen-fair", now)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "dup-1", scopeID: "scope-gate-dup", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "dup-key",
		updatedAt: now,
	})
	// The ack fence compares the lease against the database clock
	// (claim_until > clock_timestamp()), so this path runs on the real
	// clock: a fixed fixture epoch would leave every lease stillborn.
	q := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "dup-worker",
		LeaseDuration: time.Hour,
		Now:           time.Now().UTC,
	}

	first, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("first ClaimBatch() error = %v", err)
	}
	if len(first) != 1 || first[0].IntentID != "dup-1" {
		t.Fatalf("first ClaimBatch() = %v, want exactly [dup-1]", gateIntentIDs(first))
	}
	if err := q.AckBatch(ctx, first, nil); err != nil {
		t.Fatalf("AckBatch() error = %v", err)
	}
	again, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("post-ack ClaimBatch() error = %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("post-ack ClaimBatch() = %v, want empty (row is succeeded)", gateIntentIDs(again))
	}
	reopened, err := q.ReopenSucceeded(ctx, "dup-1")
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v", err)
	}
	if !reopened {
		t.Fatal("ReopenSucceeded() = false, want true")
	}
	second, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("post-reopen ClaimBatch() error = %v", err)
	}
	if len(second) != 1 || second[0].IntentID != "dup-1" {
		t.Fatalf("post-reopen ClaimBatch() = %v, want exactly [dup-1]", gateIntentIDs(second))
	}
	third, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("second post-reopen ClaimBatch() error = %v", err)
	}
	if len(third) != 0 {
		t.Fatalf("second post-reopen ClaimBatch() = %v, want empty (lease is live)", gateIntentIDs(third))
	}
}

// TestClaimBatchGatePrecomputeEmptyQueue is the C-3 empty-queue row: a claim
// against a fully-fenced (here: empty) queue returns empty with no error.
func TestClaimBatchGatePrecomputeEmptyQueue(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the gate-precompute empty-queue proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	q := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "empty-worker",
		LeaseDuration: time.Minute,
		Now:           time.Now().UTC,
	}
	intents, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("empty ClaimBatch() error = %v, want nil", err)
	}
	if len(intents) != 0 {
		t.Fatalf("empty ClaimBatch() = %v, want empty", gateIntentIDs(intents))
	}
}

// TestClaimBatchGatePrecomputeDeadLetterReplay is the C-3 dead-letter row: a
// dead-lettered row stays fenced out of the claim, and after the operator
// replay reset (status back to pending with attempts reset — no API owns
// that transition, it is the documented operator procedure) the next claim
// admits it exactly once.
func TestClaimBatchGatePrecomputeDeadLetterReplay(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the gate-precompute dead-letter proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGateScope(t, ctx, db, "scope-gate-dl", "gen-fair", now)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "dl-1", scopeID: "scope-gate-dl", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "dl-key",
		updatedAt: now,
	})
	// Simulate an exhausted Fail: attempts past budget, terminally parked.
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items
SET status = 'dead_letter', attempt_count = 5, lease_owner = NULL, claim_until = NULL, visible_at = NULL
WHERE work_item_id = 'dl-1'`)
	q := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "dl-worker",
		LeaseDuration: time.Hour,
		Now:           func() time.Time { return now.Add(2 * time.Hour) },
	}

	fenced, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("dead-letter ClaimBatch() error = %v", err)
	}
	if len(fenced) != 0 {
		t.Fatalf("dead-letter ClaimBatch() = %v, want empty (dead_letter is fenced)", gateIntentIDs(fenced))
	}
	// Operator replay: back to pending with attempts reset.
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items
SET status = 'pending', attempt_count = 0, lease_owner = NULL, claim_until = NULL, visible_at = NULL,
    failure_class = NULL, failure_message = NULL, updated_at = $1
WHERE work_item_id = 'dl-1'`, now.Add(2*time.Hour))
	replayed, err := q.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("post-replay ClaimBatch() error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].IntentID != "dl-1" {
		t.Fatalf("post-replay ClaimBatch() = %v, want exactly [dl-1]", gateIntentIDs(replayed))
	}
}

func gateIntentIDs(intents []reducercontract.Intent) []string {
	ids := make([]string, 0, len(intents))
	for _, in := range intents {
		ids = append(ids, in.IntentID)
	}
	return ids
}
