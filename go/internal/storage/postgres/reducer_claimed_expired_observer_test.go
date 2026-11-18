// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestQueueObserverStoreReducerClaimedExpiredCounts(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{int64(6), 18240.0}}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	count, age, err := observer.ReducerClaimedExpiredCounts(context.Background())
	if err != nil {
		t.Fatalf("ReducerClaimedExpiredCounts() error = %v", err)
	}
	if count != 6 {
		t.Fatalf("ReducerClaimedExpiredCounts() count = %d, want 6", count)
	}
	if age != 18240.0 {
		t.Fatalf("ReducerClaimedExpiredCounts() age = %v, want 18240", age)
	}
}

func TestQueueObserverStoreReducerClaimedExpiredCountsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{int64(0), float64(0)}}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	count, age, err := observer.ReducerClaimedExpiredCounts(context.Background())
	if err != nil {
		t.Fatalf("ReducerClaimedExpiredCounts() error = %v", err)
	}
	if count != 0 || age != 0 {
		t.Fatalf("ReducerClaimedExpiredCounts() = (%d, %v), want (0, 0)", count, age)
	}
}

func TestQueueObserverStoreReducerClaimedExpiredCountsQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("connection lost")},
		},
	}

	observer := NewQueueObserverStore(queryer)
	_, _, err := observer.ReducerClaimedExpiredCounts(context.Background())
	if err == nil {
		t.Fatal("ReducerClaimedExpiredCounts() error = nil, want non-nil")
	}
}

// TestReducerClaimedExpiredCountsLive proves the metric query against a
// live Postgres: two expired holds count with the oldest lease's exact age,
// while a live lease and a pending row stay invisible. The query text the
// hermetic pin above checks is the query that runs here.
func TestReducerClaimedExpiredCountsLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the claimed-expired metric proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	now := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-claimed-expired", now)

	insertExpired := func(id string, claimUntil time.Time) {
		t.Helper()
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     id,
			scopeID:        "scope-claimed-expired",
			generationID:   "gen-fair",
			domain:         string(reducercontract.DomainSupplyChainImpact),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    id + "-key",
			updatedAt:      now,
		})
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'metric-proof', claim_until = $2
WHERE work_item_id = $1`, id, claimUntil); err != nil {
			t.Fatalf("mark %q claimed: %v", id, err)
		}
	}

	claimAt := now.Add(2 * time.Hour)
	// Two expired holds: oldest age must be claimAt minus the older lease.
	insertExpired("metric-old", now.Add(30*time.Minute))
	insertExpired("metric-new", now.Add(90*time.Minute))
	// A live lease and a pending row: invisible to the metric.
	insertExpired("metric-live", now.Add(3*time.Hour))
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "metric-pending",
		scopeID:        "scope-claimed-expired",
		generationID:   "gen-fair",
		domain:         string(reducercontract.DomainSupplyChainImpact),
		conflictDomain: reducerConflictDomainScope,
		conflictKey:    "metric-pending-key",
		updatedAt:      now,
	})

	observer := NewQueueObserverStore(SQLDB{DB: db})
	observer.Now = func() time.Time { return claimAt }
	count, age, err := observer.ReducerClaimedExpiredCounts(ctx)
	if err != nil {
		t.Fatalf("ReducerClaimedExpiredCounts() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("ReducerClaimedExpiredCounts() count = %d, want 2 (live lease and pending row must stay invisible)", count)
	}
	// claimAt (now+2h) minus the older lease (now+30m) = 90 minutes.
	if want := 90 * 60.0; age != want {
		t.Fatalf("ReducerClaimedExpiredCounts() age = %v, want %v", age, want)
	}
}

// TestReducerClaimedExpiredQueryMirrorsReclaimPredicate pins the metric to
// the claim's reclaim class: it must count exactly the expired
// claimed/running rows the claim floats first (#10865) — stage reducer,
// claimed-or-running status, lease at or before the read clock — and must
// not merge live leases in.
func TestReducerClaimedExpiredQueryMirrorsReclaimPredicate(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"stage = 'reducer'",
		"status IN ('claimed', 'running')",
		"claim_until <= $1",
	} {
		if !strings.Contains(reducerClaimedExpiredQuery, want) {
			t.Fatalf("claimed-expired query lost the reclaim predicate %q", want)
		}
	}
	if strings.Contains(reducerClaimedExpiredQuery, "claim_until > $1") {
		t.Fatal("claimed-expired query must not count live leases")
	}
}
