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

// supplyChainImpactWriteSupersededTestError stands in for the writer's
// supplyChainImpactWriteSupersededError: a retryable admission rejection
// self-classified with the supply_chain_impact write-superseded failure class
// (#7142). Losing this normal race must not consume the retry budget.
type supplyChainImpactWriteSupersededTestError struct{}

func (supplyChainImpactWriteSupersededTestError) Error() string {
	return "supply chain impact write superseded: a fresher pass already admitted"
}

func (supplyChainImpactWriteSupersededTestError) Retryable() bool { return true }

func (supplyChainImpactWriteSupersededTestError) FailureClass() string {
	return reducer.SupplyChainImpactWriteSupersededFailureClass
}

// TestReducerQueueFailDefersSupplyChainImpactWriteSupersededPastAttemptBudget
// proves that even at an attempt count far past MaxAttempts a superseded impact
// write is deferred, never dead-lettered: dead-lettering would freeze stale
// truth for a scope that merely lost an ordinary race. Fails red without
// reducer.SupplyChainImpactWriteSupersededFailureClass registered in
// nonCountingReducerRetryFailureClasses.
func TestReducerQueueFailDefersSupplyChainImpactWriteSupersededPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{IntentID: "intent-impact-write-superseded-1", AttemptCount: 42}

	if err := queue.Fail(context.Background(), intent, supplyChainImpactWriteSupersededTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (a non-counting defer, not a dead-letter)", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'", "next_attempt_at = $5", "failure_class = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], reducer.SupplyChainImpactWriteSupersededFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimDoesNotCountSupplyChainImpactWriteSuperseded proves the
// claim path leaves attempt_count alone for the superseded class, so a retry of
// a rejected pass does not erode its budget on the next claim either.
func TestReducerQueueClaimDoesNotCountSupplyChainImpactWriteSuperseded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}
	if _, claimed, err := queue.Claim(context.Background()); err != nil || claimed {
		t.Fatalf("Claim() = claimed %v, err %v; want an empty claim", claimed, err)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"work.failure_class = 'supply_chain_impact_write_superseded'",
		"THEN work.attempt_count",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing the supply_chain_impact non-counting predicate %q:\n%s", want, query)
		}
	}
}
