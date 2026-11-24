// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11350 TEST1 (and #11297 exit-1): a 4-intent FailBatch with one 600-scope fence parks every item under the btree row limit and returns nil instead of the fatal 54000; disposable database per test, about 10 s locally
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestFailBatchWideBlockingSetParksCleanlyLive is the batch-path regression
// test for #11297's fatal Fail: N claimed intents failed in one FailBatch
// where one carries a hundreds-strong blocking set must all park under the
// btree index-row budget, and FailBatch must return nil so the service treats
// the batch as retried instead of fatal.
//
// Pre-fix this is RED: the wide item's park INSERT exceeds the 2704-byte
// btree row limit, Postgres raises SQLSTATE 54000, FailBatch returns a
// wrapped "batch fail item" error, and the batch worker reads a broken Fail
// as fatal (runMainLoop error -> main exit 1). Post-fix the narrowed wait
// cuts the blocking set to a prefix that fits, so every item parks and all
// rows retry on the ~10m backstop.
//
// Hermetic intent-count ceilings (2 intents per Fail-path test) never
// exercised this: the production failure needs a real Postgres btree
// enforcing the row limit behind a blocking set no fake cheaply models,
// which is why the regression shipped past unit tests and only fired on QA
// backlog traffic.
func TestFailBatchWideBlockingSetParksCleanlyLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)

	holders := randomHolderIDs(t, 600)
	boundFleet(t, ctx, db, at, holders)

	const items = 4
	const wideIdx = 1
	ids := []string{
		"wait-bw-batch-1",
		"wait-bw-batch-2",
		"wait-bw-batch-3",
		"wait-bw-batch-4",
	}
	clock := at
	q := newOwnGateQueue(db, &clock, reducer.DomainDeployableUnitCorrelation)
	for _, id := range ids {
		addOwnGateItem(t, ctx, db, id, boundWaiterScope, boundWaiterGen, reducer.DomainDeployableUnitCorrelation, at)
	}

	// Claim every item: FailBatch matches claimed rows only.
	clock = clock.Add(WaitingBackstopDelay + time.Minute)
	intents := make([]reducercontract.Intent, 0, items)
	for range ids {
		intent, ok, err := q.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		intents = append(intents, intent)
	}
	if len(intents) != items {
		t.Fatalf("claimed %d intents, want %d", len(intents), items)
	}

	causes := make([]error, items)
	for i := range causes {
		blocking := []string{"repo-tiny-a", "repo-tiny-b", "repo-tiny-c"}
		if i == wideIdx {
			blocking = holders
		}
		causes[i] = narrowCapCause{repos: []string{"repo-bw"}, blocking: blocking}
	}

	if err := q.FailBatch(ctx, intents, causes); err != nil {
		t.Fatalf("FailBatch with a %d-holder blocking set: %v (a broken Fail is fatal to the batch worker)", len(holders), err)
	}

	for i, id := range ids {
		status, attempts, _ := narrowQueueState(t, ctx, db, id)
		if status != "retrying" || attempts != 1 {
			t.Fatalf("queue row %s = %s attempts %d, want retrying/1", id, status, attempts)
		}
		if i != wideIdx {
			continue
		}
		// The wide item must park exactly like the single-Fail path: one
		// wait row holding a non-empty cut of the fence under the budget,
		// and the ~10m backstop horizon (proves the narrowed, not the
		// coarse, path ran for the item that could overflow the btree).
		key, size, waits := waitKey(t, ctx, db, id)
		if waits != 1 {
			t.Fatalf("wait rows for wide item = %d, want 1", waits)
		}
		if got := len(key.BlockingScopeIDs); got == 0 || got >= len(holders) {
			t.Fatalf("wide wait holds %d of %d blocking ids, want a non-empty cut", got, len(holders))
		}
		if row := len(id) + len(WaitingKindRelationshipGenerationActive) + size; row > waitingIndexRowBudgetBytes {
			t.Fatalf("wide index row = %d bytes, want <= %d", row, waitingIndexRowBudgetBytes)
		}
		_, _, visibleAt := narrowQueueState(t, ctx, db, id)
		if horizon := visibleAt.Sub(clock); horizon < 9*time.Minute || horizon > 11*time.Minute {
			t.Fatalf("wide item visible-at horizon = %v, want the ~10m backstop", horizon)
		}
	}
}
