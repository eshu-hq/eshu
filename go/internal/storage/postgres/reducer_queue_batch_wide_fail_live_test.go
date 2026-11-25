// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11350 TEST1 (and #11297 exit-1): a 4-intent FailBatch with one 600-scope fence parks every item under the btree row limit and returns nil instead of the fatal 54000; disposable database per test, about 10 s locally
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// TestFailBatchScaledAllWideParksCleanlyLive is the #11354 row
// batch:fail-batch scaled proof: a 64-intent FailBatch (the production cap:
// Service.batchClaimSize is Workers*4 capped at 64) where every intent is
// wide closes the gap the 4-intent fixture leaves open. All 64 intents carry
// the 600-holder fence at code-derived maxima ID byte lengths (work_item_ids
// built like the longest realistic production id in
// TestNarrowedWaitWorstCaseKeyFitsLive, 156 bytes: reducer_ + scope +
// generation + domain + entity key per reducerWorkItemID), and the last
// intent carries one blocking id long enough (5000 bytes) to force the
// Unrepresentable edge. FailBatch must return nil, every queue row must be
// retrying with attempts incremented, every parked wait index row must stay
// under the 2000-byte budget, the Unrepresentable item must park nothing on
// its ~10m backstop, and no 54000 may surface.
//
// Parameter source: code-derived maxima (reducerWorkItemID construction in
// reducer_queue_helpers.go, the realistic-id shape in
// reducer_waiting_narrow_bound_live_test.go). The remote read-only shape
// probe was unreachable (no cluster route from this host), so no remote
// fence histogram feeds these parameters.
func TestFailBatchScaledAllWideParksCleanlyLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)

	holders := randomHolderIDs(t, 600)
	boundFleet(t, ctx, db, at, holders)

	const items = 64
	ids := make([]string, items)
	for i := range ids {
		ids[i] = fmt.Sprintf("reducer_git-repository-scope_repository_r_%08d_%s_workload_materialization_repo_r_%08d",
			i, strings.Repeat("ab", 32), i)
	}
	t.Logf("scaled work_item_id length %d bytes", len(ids[0]))
	if len(ids[0]) != 156 {
		t.Fatalf("scaled work_item_id length = %d bytes, want 156 (code-derived maxima)", len(ids[0]))
	}
	clock := at
	q := newOwnGateQueue(db, &clock, reducer.DomainDeployableUnitCorrelation)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("failbatch-scaled-live"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	q.Instruments = instruments
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

	const unrepresentableIdx = items - 1
	causes := make([]error, items)
	for i := range causes {
		blocking := holders
		if i == unrepresentableIdx {
			blocking = []string{strings.Repeat("u", 5000)}
		}
		causes[i] = narrowCapCause{repos: []string{"repo-bw"}, blocking: blocking}
	}

	if err := q.FailBatch(ctx, intents, causes); err != nil {
		t.Fatalf("FailBatch scaled all-wide (%d intents): %v", items, err)
	}

	for i, id := range ids {
		status, attempts, visibleAt := narrowQueueState(t, ctx, db, id)
		if status != "retrying" || attempts != 1 {
			t.Fatalf("queue row %s = %s attempts %d, want retrying/1", id, status, attempts)
		}
		if horizon := visibleAt.Sub(clock); horizon < 9*time.Minute || horizon > 11*time.Minute {
			t.Fatalf("item %s visible-at horizon = %v, want the ~10m backstop", id, horizon)
		}
		if i == unrepresentableIdx {
			if _, _, waits := waitKey(t, ctx, db, id); waits != 0 {
				t.Fatalf("unrepresentable item wait rows = %d, want 0 (parks nothing)", waits)
			}
			continue
		}
		key, size, waits := waitKey(t, ctx, db, id)
		if waits != 1 {
			t.Fatalf("wait rows for wide item %s = %d, want 1", id, waits)
		}
		if got := len(key.BlockingScopeIDs); got == 0 || got >= len(holders) {
			t.Fatalf("wide wait holds %d of %d blocking ids, want a non-empty cut", got, len(holders))
		}
		if row := len(id) + len(WaitingKindRelationshipGenerationActive) + size; row > waitingIndexRowBudgetBytes {
			t.Fatalf("wide index row = %d bytes, want <= %d", row, waitingIndexRowBudgetBytes)
		}
	}

	// The Unrepresentable edge must be observed, not inferred: nil error plus
	// zero wait rows is also what a swallowed 54000 looks like through the
	// failIntent survival branch. The whole-row cap working means the closed
	// reason counts "unrepresentable" once and never "index_row_rejected".
	if got := keyBoundedCounts(t, reader); got["unrepresentable"] != 1 || got["index_row_rejected"] != 0 {
		t.Fatalf("scaled closed reasons = %v, want unrepresentable=1 and index_row_rejected=0: the whole-row cap must refuse the edge item before Postgres does", got)
	}
}
