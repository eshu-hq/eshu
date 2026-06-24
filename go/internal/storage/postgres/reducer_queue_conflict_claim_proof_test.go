// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Write-conflict handling proof for issue #3558.
//
// The reducer claim path is the durable defense against concurrent MERGE races,
// commit-time uniqueness conflicts, and lost/duplicate graph writes. Two reducer
// intents that touch the same graph hot spot share a (conflict_domain,
// conflict_key) fence (reducerConflictDomainKey). The claim query
// (claimReducerWorkQuery) must guarantee that two concurrent claimers can never
// both hold a live lease on the same conflict key at once, while intents on
// disjoint conflict keys must still be claimable concurrently so the fence is a
// partition-by-conflict-key design and not serialization-as-a-fix (AGENTS.md
// "Serialization Is Not A Fix").
//
// These proofs run real concurrent Claim() callers against a live Postgres and
// the production claim SQL. They are skipped unless a DSN is provided so the
// package unit suite stays hermetic. Run with -race to catch data races in the
// concurrent claim drivers.

const conflictProofClaimDomain = reducer.DomainCodeCallMaterialization

// TestReducerClaimFencesConcurrentClaimersOnSharedConflictKey proves that when
// two distinct reducer work items share one (conflict_domain, conflict_key),
// many concurrent claimers can never both hold a live lease on that key at the
// same time. A regression that dropped the conflict fence (or reduced it to a
// non-atomic check) would let a second claimer grab the sibling item while the
// first lease is still live, producing the concurrent-MERGE / commit-time
// uniqueness conflict this issue targets.
func TestReducerClaimFencesConcurrentClaimersOnSharedConflictKey(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the write-conflict claim proof")
	}

	ctx := context.Background()
	db, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)

	now := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-shared", now)

	// Two work items, distinct work_item_id, sharing one conflict key. They are
	// the two source runs that would race on the same code-graph nodes.
	const sharedKey = "scope-shared"
	for i := 0; i < 2; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("shared-%03d", i),
			scopeID:        "scope-shared",
			generationID:   "gen-fair",
			domain:         string(conflictProofClaimDomain),
			conflictDomain: reducerConflictDomainCodeGraph,
			conflictKey:    sharedKey,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}

	// Many claimers hammer the queue at once with a long lease so any item a
	// claimer wins stays live for the duration of the race. Each claimer gets
	// its OWN Postgres connection to the shared schema so their claim statements
	// truly interleave at the database — sharing one pooled connection would
	// serialize them and make this fence proof vacuous.
	const claimers = 8
	queues := make([]ReducerQueue, claimers)
	for i := range queues {
		claimerDB := openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)
		queues[i] = ReducerQueue{
			db:            SQLDB{DB: claimerDB},
			LeaseOwner:    fmt.Sprintf("conflict-claimer-%d", i),
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now.Add(2 * time.Hour) },
			ClaimDomains:  []reducer.Domain{conflictProofClaimDomain},
		}
	}

	var (
		mu          sync.Mutex
		liveByOwner = map[string]string{} // owner -> work_item_id currently held
		maxLive     int32
		claimedIDs  = map[string]int{}
		start       = make(chan struct{})
		wg          sync.WaitGroup
	)

	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(q ReducerQueue) {
			defer wg.Done()
			<-start
			intent, claimed, err := q.Claim(ctx)
			if err != nil {
				t.Errorf("Claim() error = %v", err)
				return
			}
			if !claimed {
				return
			}
			mu.Lock()
			liveByOwner[q.LeaseOwner] = intent.IntentID
			claimedIDs[intent.IntentID]++
			if n := int32(len(liveByOwner)); n > atomic.LoadInt32(&maxLive) {
				atomic.StoreInt32(&maxLive, n)
			}
			mu.Unlock()
		}(queues[i])
	}

	close(start)
	wg.Wait()

	// The fence guarantees at most one live lease across the shared conflict key
	// at any time. Because every winning claimer holds a minute-long lease that
	// never expires during the race, the count of simultaneously-held leases is
	// the count of distinct items claimed without release. A correct fence lets
	// at most one through.
	if got := int(atomic.LoadInt32(&maxLive)); got > 1 {
		t.Fatalf("simultaneous live leases on shared conflict key = %d, want <= 1; "+
			"conflict fence let concurrent claimers race the same graph hot spot (issue #3558): claimed=%v",
			got, claimedIDs)
	}
	for id, n := range claimedIDs {
		if n > 1 {
			t.Fatalf("work item %q claimed %d times concurrently; duplicate claim is a lost/duplicate-write hazard", id, n)
		}
	}
}

// TestReducerClaimAllowsConcurrentClaimersOnDisjointConflictKeys proves the
// fence is partition-by-conflict-key, not serialization: two work items with
// disjoint conflict keys are both claimable concurrently. A "fix" that
// serialized all reducer claims (single-threaded drain) would fail this proof.
func TestReducerClaimAllowsConcurrentClaimersOnDisjointConflictKeys(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the write-conflict claim proof")
	}

	ctx := context.Background()
	db, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)

	now := time.Date(2026, time.June, 22, 11, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-disjoint", now)

	// Two work items in the same conflict domain but DISJOINT conflict keys.
	for i := 0; i < 2; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("disjoint-%03d", i),
			scopeID:        "scope-disjoint",
			generationID:   "gen-fair",
			domain:         string(conflictProofClaimDomain),
			conflictDomain: reducerConflictDomainCodeGraph,
			conflictKey:    fmt.Sprintf("disjoint-key-%03d", i),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}

	const claimers = 2
	var (
		mu      sync.Mutex
		claimed = map[string]struct{}{}
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for i := 0; i < claimers; i++ {
		// Each claimer needs its own connection so the two disjoint-key claims
		// run concurrently rather than serializing on a shared pooled connection.
		claimerDB := openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)
		q := ReducerQueue{
			db:            SQLDB{DB: claimerDB},
			LeaseOwner:    fmt.Sprintf("disjoint-claimer-%d", i),
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now.Add(2 * time.Hour) },
			ClaimDomains:  []reducer.Domain{conflictProofClaimDomain},
		}
		wg.Add(1)
		go func(q ReducerQueue) {
			defer wg.Done()
			<-start
			intent, ok, err := q.Claim(ctx)
			if err != nil {
				t.Errorf("Claim() error = %v", err)
				return
			}
			if !ok {
				return
			}
			mu.Lock()
			claimed[intent.IntentID] = struct{}{}
			mu.Unlock()
		}(q)
	}
	close(start)
	wg.Wait()

	if len(claimed) != 2 {
		t.Fatalf("distinct items claimed concurrently on disjoint conflict keys = %d, want 2; "+
			"the conflict fence must not serialize unrelated graph families (AGENTS.md Serialization Is Not A Fix): claimed=%v",
			len(claimed), claimed)
	}
}

// TestReducerClaimFencedSiblingBecomesClaimableAfterAck proves convergence with
// no lost write: a sibling fenced behind a live lease on the same conflict key
// becomes claimable once the holder acks and releases the lease. This is the
// idempotent-retry/ordering half of the proof — fenced work is deferred, never
// dropped.
func TestReducerClaimFencedSiblingBecomesClaimableAfterAck(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the write-conflict claim proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-converge", now)

	const sharedKey = "scope-converge"
	for i := 0; i < 2; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("converge-%03d", i),
			scopeID:        "scope-converge",
			generationID:   "gen-fair",
			domain:         string(conflictProofClaimDomain),
			conflictDomain: reducerConflictDomainCodeGraph,
			conflictKey:    sharedKey,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}

	claimAt := now.Add(2 * time.Hour)
	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "converge-claimer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return claimAt },
		ClaimDomains:  []reducer.Domain{conflictProofClaimDomain},
	}

	// First claim wins one item; the sibling is fenced because the first lease
	// is live (claim_until = claimAt + 1m > claimAt).
	first, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("first Claim() claimed nothing, want one item")
	}

	fenced, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("fenced Claim() error = %v", err)
	}
	if ok {
		t.Fatalf("second Claim() returned %q while sibling lease on the same conflict key is live; "+
			"the fence must defer the sibling (issue #3558)", fenced.IntentID)
	}

	// Ack the first item; its lease is released and the sibling must now drain.
	if err := queue.Ack(ctx, first, reducer.Result{}); err != nil {
		t.Fatalf("Ack(first) error = %v", err)
	}

	released, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("post-ack Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("post-ack Claim() claimed nothing; fenced sibling was lost, not deferred (lost-write hazard, issue #3558)")
	}
	if released.IntentID == first.IntentID {
		t.Fatalf("post-ack Claim() re-claimed the acked item %q instead of the deferred sibling", first.IntentID)
	}
}
