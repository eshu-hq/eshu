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

func TestReducerQueueFailDefersSecretsIAMEndpointReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, secretsIAMEndpointNotReadyTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
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
	if got, want := db.execs[0].args[1], reducer.SecretsIAMEndpointNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], now.Add(2*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
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
		db:            db,
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
		db:            db,
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
	return reducer.SecretsIAMEndpointNotReadyFailureClass
}
