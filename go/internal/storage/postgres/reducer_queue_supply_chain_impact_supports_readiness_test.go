// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// supportsNotReadyClass is the failure class the supports readers' not-ready
// refusal carries (#10880 PR-B2, arbiter-b2c decision 2).
const supportsNotReadyClass = "supply_chain_impact_supports_not_ready"

// TestReducerQueueFailDefersSupplyChainImpactSupportsNotReadyPastAttemptBudget
// proves that, at an attempt count far past MaxAttempts, a supports-not-ready
// refusal is deferred and never dead-lettered. The supports backfill takes
// hours on a large install; a counting class would dead-letter the intent after
// about three minutes (three attempts, a 60 s then 120 s delay) and leave its
// scope's reconciliation, service-catalog correlation or holder reopen undone
// until a manual replay. It fails red unless the class is enrolled in
// nonCountingReducerRetryFailureClasses.
func TestReducerQueueFailDefersSupplyChainImpactSupportsNotReadyPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducercontract.Intent{IntentID: "intent-supports-not-ready-1", AttemptCount: 42}

	if err := queue.Fail(context.Background(), intent, supportsNotReadyTestError()); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d (a non-counting defer, not a dead-letter)", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'", "next_attempt_at = $5", "failure_class = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if got := db.queries[0].args[1]; got != supportsNotReadyClass {
		t.Fatalf("failure class = %v, want %v", got, supportsNotReadyClass)
	}
}

// TestReducerQueueClaimDoesNotCountSupplyChainImpactSupportsNotReady proves the
// claim path leaves attempt_count alone for the class, so a deferred intent
// re-claimed every minute for hours never erodes its budget, and a fourth
// cycle still reads attempt 1.
func TestReducerQueueClaimDoesNotCountSupplyChainImpactSupportsNotReady(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
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
		"work.failure_class = '" + supportsNotReadyClass + "'",
		"THEN work.attempt_count",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing the supports-not-ready non-counting predicate %q:\n%s", want, query)
		}
	}
}

// supportsNotReadyTestError is the refusal the readers return.
func supportsNotReadyTestError() error {
	return &supplychaincore.SupplyChainImpactSupportsNotReadyError{Reader: "security_alert_reconciliation"}
}
