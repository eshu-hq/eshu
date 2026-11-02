// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// advancingPinner pins successfully and, from the second call on (or on the
// first when advanceOnFirst is set), moves the store clock to `to`.
type advancingPinner struct {
	clock *atomic.Int64
	to    time.Time
	calls atomic.Int32
}

func (p *advancingPinner) PinClaimForCommit(_ context.Context, claimID string, _ int64, _, _, _ string, _ time.Time) (string, error) {
	p.calls.Add(1)
	p.clock.Store(p.to.UnixNano())
	return "commit-" + claimID, nil
}

func reprojectTestStore(database *fakeTransactionalDB, at time.Time, pinner fence.Pinner) (IngestionStore, *atomic.Int64) {
	clock := &atomic.Int64{}
	clock.Store(at.UnixNano())
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: pinner, Code: claimFenceTestCode, PinBackoff: time.Microsecond,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
	}
	return store, clock
}

func reprojectMatchedTx() *fakeTx {
	return &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{true, true}}}}}
}

// TestPlaneReprojectTakesThePinAndDeadlineFence is T10: the unchanged-
// generation re-projection under a plane-held claim pins before BEGIN,
// never heartbeats a core claim row, writes no fence row, and rolls back on
// a stale pin or a missed deadline.
func TestPlaneReprojectTakesThePinAndDeadlineFence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	scopeValue := claimFenceTestScope("scope-r")
	generation := claimFenceTestGeneration("scope-r", "gen-r", now)
	candidate := backfill.UnchangedGenerationCandidate{GenerationID: "gen-r"}
	mutation := claimFenceTestMutation("scope-r", "gen-r", "claim-r", 2)

	t.Run("committed", func(t *testing.T) {
		database := &fakeTransactionalDB{tx: reprojectMatchedTx()}
		pinner := &claimFenceFakePinner{}
		store, _ := reprojectTestStore(database, now, pinner)
		reason, err := store.reprojectActiveGeneration(context.Background(), mutation, true, scopeValue, generation, candidate)
		if err != nil || reason == "" {
			t.Fatalf("reproject = (%q, %v), want a committed re-projection", reason, err)
		}
		if !database.tx.committed || len(pinner.snapshot()) != 1 {
			t.Fatalf("committed = %v, pins = %d, want true and 1", database.tx.committed, len(pinner.snapshot()))
		}
		for _, exec := range database.tx.execs {
			if strings.Contains(exec.query, "workflow_claims") || strings.Contains(exec.query, "generation_commit_fences") {
				t.Fatalf("plane re-projection ran %q, want no core claim heartbeat and no fence row", exec.query)
			}
		}
	})
	t.Run("stale pin", func(t *testing.T) {
		database := &fakeTransactionalDB{tx: reprojectMatchedTx()}
		store, _ := reprojectTestStore(database, now, &claimFenceFakePinner{results: []error{claimFenceCodedError{"stale_fence"}}})
		_, err := store.reprojectActiveGeneration(context.Background(), mutation, true, scopeValue, generation, candidate)
		requireRejection(t, err, fence.ReasonStaleFence, true)
		if database.beginCalls != 0 {
			t.Fatalf("begin calls = %d, want 0", database.beginCalls)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		database := &fakeTransactionalDB{tx: reprojectMatchedTx()}
		clock := &atomic.Int64{}
		pinner := &advancingPinner{clock: clock, to: now.Add(time.Minute - DefaultClaimFenceMargin + time.Nanosecond)}
		store, storeClock := reprojectTestStore(database, now, pinner)
		pinner.clock = storeClock
		_, err := store.reprojectActiveGeneration(context.Background(), mutation, true, scopeValue, generation, candidate)
		requireRejection(t, err, fence.ReasonDeadline, false)
		if database.tx.committed || !database.tx.rolledBack {
			t.Fatalf("committed = %v rolledBack = %v, want a rollback", database.tx.committed, database.tx.rolledBack)
		}
	})
}

// TestInTreeClaimedCommitWritesNoFenceRow is the T8 half that the existing
// statement-sequence test does not cover: with no claim plane configured the
// claimed commit still heartbeats the core claim row first and never touches
// generation_commit_fences.
func TestInTreeClaimedCommitWritesNoFenceRow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	mutation := claimFenceTestMutation("scope-i", "gen-i", "claim-i", 7)
	err := store.CommitClaimedScopeGeneration(context.Background(), mutation, claimFenceTestScope("scope-i"),
		claimFenceTestGeneration("scope-i", "gen-i", now), testfixtures.FactChannel([]facts.Envelope{{
			FactID: "fact-i", ScopeID: "scope-i", GenerationID: "gen-i", FactKind: "pagerduty_service",
			StableFactKey: "i", ObservedAt: now,
		}}))
	if err != nil {
		t.Fatalf("in-tree commit error = %v", err)
	}
	if got := database.tx.execs[0].query; !strings.Contains(got, "workflow_claims") {
		t.Fatalf("first exec = %q, want the in-tree claim heartbeat", got)
	}
	for _, query := range database.tx.queries {
		if strings.Contains(query.query, "generation_commit_fences") {
			t.Fatalf("in-tree commit queried the claim-plane fence table: %q", query.query)
		}
	}
	for _, exec := range database.tx.execs {
		if strings.Contains(exec.query, "generation_commit_fences") {
			t.Fatalf("in-tree commit wrote the claim-plane fence table: %q", exec.query)
		}
	}
}

// ctxAwareBeginDB hands out transactions whose first query runs onQuery with
// the statement's context, so a test can act mid-transaction the way a real
// driver observes cancellation.
type ctxAwareBeginDB struct {
	*fakeTransactionalDB
	onQuery func(context.Context) error
}

func (d ctxAwareBeginDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.fakeTransactionalDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ctxAwareTx{Transaction: tx, onQuery: d.onQuery}, nil
}

type ctxAwareTx struct {
	db.Transaction
	once    sync.Once
	onQuery func(context.Context) error
}

func (t *ctxAwareTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	var hookErr error
	t.once.Do(func() { hookErr = t.onQuery(ctx) })
	if hookErr != nil {
		return nil, hookErr
	}
	return t.Transaction.QueryContext(ctx, query, args...)
}

// TestPlaneReprojectStaleRepinMidTransactionRollsBack is the T10 shape of T3:
// the first re-pin comes back stale while the re-projection transaction is
// open, the statement in flight sees its context cancelled, the transaction
// rolls back, and the error is the terminal stale refusal.
func TestPlaneReprojectStaleRepinMidTransactionRollsBack(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time)
	inner := &fakeTransactionalDB{tx: reprojectMatchedTx()}
	database := ctxAwareBeginDB{fakeTransactionalDB: inner, onQuery: func(ctx context.Context) error {
		ticks <- time.Time{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("commit context was not cancelled by the stale re-pin")
		}
	}}
	pinner := &claimFenceFakePinner{results: []error{nil, claimFenceCodedError{"stale_fence"}}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: pinner, Code: claimFenceTestCode, PinBackoff: time.Microsecond,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} },
	}
	_, err := store.reprojectActiveGeneration(context.Background(), claimFenceTestMutation("scope-r", "gen-r", "claim-r", 2), true,
		claimFenceTestScope("scope-r"), claimFenceTestGeneration("scope-r", "gen-r", now),
		backfill.UnchangedGenerationCandidate{GenerationID: "gen-r"})
	requireRejection(t, err, fence.ReasonStaleFence, true)
	if inner.tx.committed || !inner.tx.rolledBack {
		t.Fatalf("committed = %v rolledBack = %v, want a rollback", inner.tx.committed, inner.tx.rolledBack)
	}
	if got := len(pinner.snapshot()); got != 2 {
		t.Fatalf("pin calls = %d, want 2 (the pin and the stale re-pin)", got)
	}
}
