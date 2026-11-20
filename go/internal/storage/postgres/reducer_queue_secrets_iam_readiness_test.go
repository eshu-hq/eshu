// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

func TestReducerQueueFailDefersSecretsIAMEndpointReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}

	intent := reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, secretsIAMEndpointNotReadyTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	// #10879 phase 1 reports the terminal match count through
	// QueryContext, so the retry statement lands in queries while the
	// wait-row park stays an exec. The park then re-checks satisfaction,
	// so the Fail issues two queries and one exec in total.
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := db.queries[1].query; got != waitingSatisfiedQuery {
		t.Fatalf("second query is not the park satisfaction re-check:\n%s", got)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"next_attempt_at = $5",
		"visible_at = $5",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[1], secretsiam.SecretsIAMEndpointNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	// #10876: a parked readiness wait returns on the 10-minute backstop, not
	// on the exponential backoff: the wait row carries the waits-for and the
	// provider wake (or the audit) reopens it, so the backoff only bounds a
	// wait the map does not park.
	if got, want := db.queries[0].args[4], now.Add(WaitingBackstopDelay); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
	// The park writes exactly one wait row for the deferred intent.
	if got := db.execs[0].query; !strings.Contains(got, "INSERT INTO reducer_waiting_items") {
		t.Fatalf("exec is not the wait-row park: %s", got)
	}
}

func TestReducerQueueClaimDoesNotCountSecretsIAMEndpointReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	assertSecretsIAMReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

func TestClaimBatchDoesNotCountSecretsIAMEndpointReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, err := queue.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	assertSecretsIAMReadinessClaimDoesNotCountAttempt(t, db.queries[0].query)
}

func assertSecretsIAMReadinessClaimDoesNotCountAttempt(t *testing.T, query string) {
	t.Helper()

	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'secrets_iam_endpoint_not_ready'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing non-counting defer attempt predicate %q:\n%s", want, query)
		}
	}
}

type secretsIAMEndpointNotReadyTestError struct{}

func (secretsIAMEndpointNotReadyTestError) Error() string {
	return "secrets/IAM endpoint readiness is blocked"
}

func (secretsIAMEndpointNotReadyTestError) Retryable() bool {
	return true
}

func (secretsIAMEndpointNotReadyTestError) FailureClass() string {
	return secretsiam.SecretsIAMEndpointNotReadyFailureClass
}
