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
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestReducerQueueGoEvidenceChangedNeverSpendsAnAttempt is the seeded RED/GREEN pair of
// #11327 ruling 2.5. N consecutive aborts by the go-evidence recheck, each past the
// attempt budget, are deferred as a non-counting retry with the attempt count left
// alone: it fails red unless the class is enrolled in
// nonCountingReducerRetryFailureClasses. A genuine failure in the same statement
// still counts: a plain error at the same attempt count takes the dead-letter path,
// so the class list did not turn every failure into an endless retry.
func TestReducerQueueGoEvidenceChangedNeverSpendsAnAttempt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
	newQueue := func(db *fakeExecQueryer) ReducerQueue {
		return ReducerQueue{
			database: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute, RetryDelay: 2 * time.Minute,
			MaxAttempts: 3, Now: func() time.Time { return now },
		}
	}
	for aborts := 1; aborts <= 5; aborts++ {
		db := &fakeExecQueryer{}
		intent := reducercontract.Intent{IntentID: "intent-go-evidence", AttemptCount: 42}
		err := &supplychaincore.SupplyChainImpactGoEvidenceChangedError{Cause: supplychaincore.GoEvidenceCauseGenerationChanged}
		if failErr := newQueue(db).Fail(context.Background(), intent, err); failErr != nil {
			t.Fatalf("abort %d: Fail() error = %v", aborts, failErr)
		}
		if len(db.queries) != 1 || !strings.Contains(db.queries[0].query, "status = 'retrying'") ||
			strings.Contains(db.queries[0].query, "dead_letter") {
			t.Fatalf("abort %d: query = %+v, want one non-counting retrying defer", aborts, db.queries)
		}
		if got := db.queries[0].args[1]; got != supplychaincore.SupplyChainImpactGoEvidenceChangedFailureClass {
			t.Fatalf("abort %d: failure class = %v, want %v", aborts, got, supplychaincore.SupplyChainImpactGoEvidenceChangedFailureClass)
		}
	}

	genuine := &fakeExecQueryer{}
	if failErr := newQueue(genuine).Fail(context.Background(),
		reducercontract.Intent{IntentID: "intent-go-evidence", AttemptCount: 42}, errors.New("a genuine writer failure")); failErr != nil {
		t.Fatalf("genuine Fail() error = %v", failErr)
	}
	if len(genuine.queries) == 0 || !strings.Contains(genuine.queries[0].query, "dead_letter") {
		t.Fatalf("genuine failure at attempt 42 = %+v, want the dead-letter path (the class is non-counting, nothing else is)", genuine.queries)
	}
}

// TestReducerQueueClaimDoesNotCountGoEvidenceChanged proves the claim path leaves
// attempt_count alone for the class, so an intent re-claimed after each abort never
// erodes its budget.
func TestReducerQueueClaimDoesNotCountGoEvidenceChanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	queue := ReducerQueue{database: db, LeaseOwner: "test-owner", LeaseDuration: 30 * time.Second, Now: func() time.Time { return now }}
	if _, claimed, err := queue.Claim(context.Background()); err != nil || claimed {
		t.Fatalf("Claim() = claimed %v, err %v; want an empty claim", claimed, err)
	}
	if !strings.Contains(db.queries[0].query, "work.failure_class = '"+supplychaincore.SupplyChainImpactGoEvidenceChangedFailureClass+"'") {
		t.Fatalf("claim query missing the go-evidence non-counting predicate:\n%s", db.queries[0].query)
	}
}
