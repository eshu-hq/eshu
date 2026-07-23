// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCrossScopeProducerNotReadyIsNonCounting asserts the #5709 cross-scope
// readiness class is exempt from the reducer retry budget on the Go side, in
// lockstep with the SQL claim path below. A retrying row deferred because its
// producer scope has not activated must not burn its attempt budget.
func TestCrossScopeProducerNotReadyIsNonCounting(t *testing.T) {
	t.Parallel()

	if !IsNonCountingReducerRetryFailureClass(reducer.CrossScopeProducerNotReadyFailureClass) {
		t.Fatalf("%q must be exempt from the reducer retry budget", reducer.CrossScopeProducerNotReadyFailureClass)
	}
}

// TestReducerQueueClaimDoesNotCountCrossScopeReadinessDefers asserts the claim
// query's attempt-count CASE keeps attempt_count for a retrying row in the
// cross-scope readiness class. The freeze behavior is proven end-to-end against
// Postgres in docs/internal/evidence/5709-attempt-count-freeze.md; this guards
// that the shared single-source class list still renders the class into the
// claim query so the two never drift.
func TestReducerQueueClaimDoesNotCountCrossScopeReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	if _, claimed, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'cross_scope_producer_not_ready'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing cross-scope non-counting defer predicate %q:\n%s", want, query)
		}
	}
}
