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

// awsCloudRuntimeDriftStatePendingTestError stands in for the handler's
// awsCloudRuntimeDriftStatePendingError: a retryable readiness-gate defer
// that self-classifies with the aws_cloud_runtime_drift state-pending
// failure class. The queue must defer it without consuming the retry budget,
// mirroring the GCP/EC2/secrets-IAM/kubernetes-correlation non-counting
// contracts, so a Terraform state_snapshot scope still mid-ingestion can
// never dead-letter a still-pending drift intent the succeeded-only reopen
// path would not reopen.
type awsCloudRuntimeDriftStatePendingTestError struct{}

func (awsCloudRuntimeDriftStatePendingTestError) Error() string {
	return "aws cloud runtime drift deferred: terraform state_snapshot scope still pending"
}

func (awsCloudRuntimeDriftStatePendingTestError) Retryable() bool { return true }

func (awsCloudRuntimeDriftStatePendingTestError) FailureClass() string {
	return reducer.AWSCloudRuntimeDriftStatePendingFailureClass
}

// awsCloudRuntimeDriftWriteSupersededTestError stands in for the writer's
// awsCloudRuntimeDriftWriteSupersededError: a retryable insert-admission
// rejection self-classified with the aws_cloud_runtime_drift write-superseded
// failure class. Losing this normal admission race must not consume the
// retry budget either -- a stalled worker admitted-then-rejected still gets
// to retry with fresh evidence.
type awsCloudRuntimeDriftWriteSupersededTestError struct{}

func (awsCloudRuntimeDriftWriteSupersededTestError) Error() string {
	return "aws cloud runtime drift write superseded: a fresher pass already admitted"
}

func (awsCloudRuntimeDriftWriteSupersededTestError) Retryable() bool { return true }

func (awsCloudRuntimeDriftWriteSupersededTestError) FailureClass() string {
	return reducer.AWSCloudRuntimeDriftWriteSupersededFailureClass
}

// TestReducerQueueFailDefersAWSCloudRuntimeDriftStatePendingPastAttemptBudget
// proves the Go retry classifier treats an aws_cloud_runtime_drift
// state-pending defer as non-counting: even at an attempt count far past
// MaxAttempts, Fail re-queues the row as retrying rather than dead-lettering
// it. This is the P0 regression: it fails red without
// reducer.AWSCloudRuntimeDriftStatePendingFailureClass registered in
// nonCountingReducerRetryFailureClasses, in which case Fail would dead-letter
// instead of deferring -- turning aws_cloud_runtime_drift's own bounded
// readiness defer (capped at awsCloudRuntimeDriftStatePendingMaxWait, 30
// minutes of elapsed wall-clock time since Intent.CycleStartedAt -- NOT a
// retry-count bound; a retry-count bound is unreachable once this exact
// failure class is registered as non-counting below, since the registration
// freezes attempt_count) into a permanent, unrecoverable data loss for the
// ARN once the QUEUE's own MaxAttempts is reached first, rather than the
// domain's own terminal fallback ever getting to commit a best-available
// verdict.
// Mirrors TestReducerQueueFailDefersGCPRelationshipReadinessPastAttemptBudget
// and TestReducerQueueFailDefersEC2InstanceIdentityReadinessPastAttemptBudget.
func TestReducerQueueFailDefersAWSCloudRuntimeDriftStatePendingPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 11, 0, 0, 0, time.UTC)
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
		IntentID:     "intent-aws-drift-state-pending-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, awsCloudRuntimeDriftStatePendingTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (a non-counting defer, not a dead-letter)", got, want)
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
	if got, want := db.execs[0].args[1], reducer.AWSCloudRuntimeDriftStatePendingFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerQueueFailDefersAWSCloudRuntimeDriftWriteSupersededPastAttemptBudget
// is the write-admission sibling of the test above: even at an attempt count
// far past MaxAttempts, a superseded-write rejection must be deferred, not
// dead-lettered. Fails red without
// reducer.AWSCloudRuntimeDriftWriteSupersededFailureClass registered in
// nonCountingReducerRetryFailureClasses.
func TestReducerQueueFailDefersAWSCloudRuntimeDriftWriteSupersededPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 11, 5, 0, 0, time.UTC)
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
		IntentID:     "intent-aws-drift-write-superseded-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, awsCloudRuntimeDriftWriteSupersededTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (a non-counting defer, not a dead-letter)", got, want)
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
	if got, want := db.execs[0].args[1], reducer.AWSCloudRuntimeDriftWriteSupersededFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}

// TestReducerQueueClaimDoesNotCountAWSCloudRuntimeDriftReadinessDefers asserts
// the claim query's attempt-count CASE keeps attempt_count for a retrying row
// in EITHER new aws_cloud_runtime_drift class, so both claim paths (single and
// batch) agree with the Go-side Fail() classification above and the two can
// never drift apart.
func TestReducerQueueClaimDoesNotCountAWSCloudRuntimeDriftReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
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
		"work.failure_class = 'aws_cloud_runtime_drift_state_pending'",
		"work.failure_class = 'aws_cloud_runtime_drift_write_superseded'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing aws_cloud_runtime_drift non-counting defer predicate %q:\n%s", want, query)
		}
	}
}

// TestClaimBatchDoesNotCountAWSCloudRuntimeDriftReadinessDefers is the batch
// claim path's sibling of the test above: both claim paths derive their
// attempt-count CASE from the same reducerNonCountingFailureClassPredicateSQL,
// so they must never disagree on which classes are exempt.
func TestClaimBatchDoesNotCountAWSCloudRuntimeDriftReadinessDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 12, 5, 0, 0, time.UTC)
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

	query := db.queries[0].query
	for _, want := range []string{
		"attempt_count = CASE",
		"work.status = 'retrying'",
		"work.failure_class = 'aws_cloud_runtime_drift_state_pending'",
		"work.failure_class = 'aws_cloud_runtime_drift_write_superseded'",
		"THEN work.attempt_count",
		"ELSE work.attempt_count + 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing aws_cloud_runtime_drift non-counting defer predicate %q:\n%s", want, query)
		}
	}
}
