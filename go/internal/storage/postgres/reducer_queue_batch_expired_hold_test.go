// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// expiredClaimedWorkItemIDs returns the work_item_ids whose expired
// claimed/running lease puts them at reclaim_rank 0 under the #10865 order
// at the given claim clock. It reads the rows, not the implementation, so
// the differentials that use it prove the new order against an independent
// expectation instead of restating the shipped CASE.
func expiredClaimedWorkItemIDs(t *testing.T, ctx context.Context, db *sql.DB, clock time.Time) map[string]bool {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT work_item_id FROM fact_work_items
WHERE stage = 'reducer' AND status IN ('claimed', 'running') AND claim_until <= $1`, clock)
	if err != nil {
		t.Fatalf("read expired claimed rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	expired := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan expired claimed row: %v", err)
		}
		expired[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate expired claimed rows: %v", err)
	}
	return expired
}

// reclaimFirstExpectedOrder stable-partitions ids the way the #10865 order
// must: expired holds first in their old relative order, then every other
// row in its old relative order. The differentials keep their 0/0 set check
// and compare the new order against this instead of byte-identical order,
// because floating expired holds first IS the shipped behavior change.
func reclaimFirstExpectedOrder(ids []string, expired map[string]bool) []string {
	var first, rest []string
	for _, id := range ids {
		if expired[id] {
			first = append(first, id)
		} else {
			rest = append(rest, id)
		}
	}
	return append(first, rest...)
}

// TestClaimBatchExpiredHoldClaimedFirst is the #10865 regression proof
// (RED before the reclaim_rank fix, GREEN after): one expired claimed row
// behind a deeper pending backlog must be admitted within the first batch,
// not starved behind it.
//
// Ruled-out paths (recorded, not re-tested here): under a healthy run every
// claimed row either reaches a worker over the unbuffered workCh rendezvous
// or is dropped at a recorded shutdown/panic site whose unrenewed lease
// expires for reclaim (service_batch.go claim-wins-race, mid-handoff cancel,
// post-receive shutdown). No path marks a batch claimed without handing it
// to workers and without recording the drop, so the production shape —
// claimed rows sitting 5h past lease expiry while 24 same-group items wait —
// is ORDER BY starvation, not a lost handoff: the expired hold rewrote its
// own updated_at at claim time, so it sorts AFTER older pending rows at the
// updated_at ASC tie-break and perpetually loses the LIMIT slots.
func TestClaimBatchExpiredHoldClaimedFirst(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the expired-hold starvation proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain = reducercontract.DomainSupplyChainImpact
		busyCount  = 24
		batchSize  = 8
	)

	now := time.Date(2026, time.November, 18, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-expired-hold", now)

	// Deeper pending backlog, each row in its own conflict group so the
	// representative fence never collapses them. Older than the expired
	// hold below, which is the starvation shape: the hold claimed (and
	// rewrote updated_at) after the backlog was enqueued.
	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("busy-%03d", i),
			scopeID:        "scope-expired-hold",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("busy-key-%03d", i),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	// The expired hold: claimed long ago, lease lapsed, updated_at newer
	// than the whole backlog (the claim rewrote it). Its own conflict key,
	// so it is its key's same_rn = 1 representative and only the
	// cross-group LIMIT order can bury it.
	insertReducerFairnessClaimedWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "expired-hold",
		scopeID:        "scope-expired-hold",
		generationID:   "gen-fair",
		domain:         string(busyDomain),
		conflictDomain: reducerConflictDomainScope,
		conflictKey:    "expired-hold-key",
		updatedAt:      now.Add(time.Duration(busyCount) * time.Second),
	}, now.Add(time.Hour)) // claim_until: 1h after seed, expired at claim time.

	claimAt := now.Add(2 * time.Hour)
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "expired-hold-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return claimAt },
		ClaimDomains:  []reducercontract.Domain{busyDomain},
	}

	intents, err := queue.ClaimBatch(ctx, batchSize)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != batchSize {
		t.Fatalf("ClaimBatch() claimed %d items, want a full batch of %d "+
			"(fixture must hold backlog pressure: %d pending + 1 expired hold)",
			len(intents), batchSize, busyCount)
	}

	for _, intent := range intents {
		if intent.IntentID == "expired-hold" {
			return
		}
	}
	got := make([]string, 0, len(intents))
	for _, intent := range intents {
		got = append(got, intent.IntentID)
	}
	t.Fatalf("ClaimBatch() batch of %d has no expired hold; expired claimed "+
		"row starves behind the deeper pending backlog (issue #10865): %v",
		len(intents), got)
}

// TestClaimBatchConcurrentSameConflictGroupAdmitsExpiredOnce is the #10865
// acceptance-(3) proof: several claims racing for the same conflict group as
// one expired hold must resolve to exactly one winner for the hold — no
// duplicate delivery and no second live lease on the key — while the losers
// still make progress on other keys. It runs GREEN before and after the
// reclaim_rank fix (the fence, not the order, owns it); it pins the fence
// while the companion test above pins the order.
func TestClaimBatchConcurrentSameConflictGroupAdmitsExpiredOnce(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the expired-hold contention proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)

	const (
		busyDomain = reducercontract.DomainSupplyChainImpact
		otherCount = 7
		claimers   = 4
		rounds     = 6
	)

	now := time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-expired-race", now)

	// One expired hold on the contended key, plus other keys so losing
	// claimers have somewhere to make progress.
	insertReducerFairnessClaimedWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "race-expired-hold",
		scopeID:        "scope-expired-race",
		generationID:   "gen-fair",
		domain:         string(busyDomain),
		conflictDomain: reducerConflictDomainScope,
		conflictKey:    "race-key",
		updatedAt:      now,
	}, now.Add(time.Hour))
	for i := 0; i < otherCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("race-other-%02d", i),
			scopeID:        "scope-expired-race",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("race-other-key-%02d", i),
			updatedAt:      now.Add(time.Duration(i+1) * time.Second),
		})
	}

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
				LeaseOwner:    fmt.Sprintf("race-claimer-%d", i),
				LeaseDuration: time.Hour,
				Now:           func() time.Time { return claimAt },
				ClaimDomains:  []reducercontract.Domain{busyDomain},
			}
			<-start
			for r := 0; r < rounds; r++ {
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

	if got := claimed["race-expired-hold"]; got != 1 {
		t.Fatalf("expired hold delivered %d times, want exactly once "+
			"(competing claims must resolve to one winner)", got)
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
		t.Fatalf("%d conflict keys hold more than one live lease after the race", dupKeys)
	}
}

// TestClaimBatchExpiredHoldOrderedFirstInShippedQuery is the hermetic
// companion to the live starvation proof: it pins the shipped ordering
// fragments that put an expired claimed/running hold ahead of ordinary
// waiting rows at the batch LIMIT, mirroring the projector #10480
// reclaim_rank. The priority CASE puts every expired claimed/running row at
// rank 0 and everything else at 1; the lock arms sort that key first, so a
// deeper pending backlog can no longer bury the hold.
func TestClaimBatchExpiredHoldOrderedFirstInShippedQuery(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery

	const reclaimCASE = "CASE WHEN reps.status IN ('claimed', 'running') THEN 0 ELSE 1 END AS reclaim_rank"
	if !strings.Contains(query, reclaimCASE) {
		t.Fatalf("batch claim query lost the expired-lease reclaim rank %q", reclaimCASE)
	}
	for _, want := range []string{
		"ORDER BY candidate.reclaim_rank ASC, candidate.reducer_domain_priority ASC",
		"ORDER BY candidate.reclaim_rank ASC, candidate.updated_at ASC, candidate.work_item_id ASC",
		"ORDER BY ordered_candidate.reclaim_rank ASC, ordered_candidate.updated_at ASC, ordered_candidate.work_item_id ASC",
		"ORDER BY reclaim_rank ASC, reducer_domain_priority ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query no longer orders expired holds first with %q", want)
		}
	}

	single := claimReducerWorkQuery
	const singleReclaim = "CASE WHEN fact_work_items.status IN ('claimed', 'running') AND fact_work_items.claim_until <= $1 THEN 0 ELSE 1 END AS reclaim_rank"
	if !strings.Contains(single, singleReclaim) {
		t.Fatalf("single claim query lost the expired-lease reclaim rank %q", singleReclaim)
	}
	if !strings.Contains(single, "ORDER BY reclaim_rank ASC, updated_at ASC, work_item_id ASC") {
		t.Fatalf("single claim query no longer orders expired holds first")
	}
}
