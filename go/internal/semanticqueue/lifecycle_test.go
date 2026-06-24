// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticqueue_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

func TestRecordRetryAndDeadLetterAreBounded(t *testing.T) {
	t.Parallel()

	plan, err := semanticqueue.BuildPlan(basePlanRequest())
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}
	job := plan.Jobs[0]
	now := fixedTime().Add(time.Minute)
	next := now.Add(5 * time.Minute)
	retry, err := semanticqueue.Retry(job, now, next, semanticqueue.Failure{
		Class:   semanticqueue.FailureClassProviderUnavailable,
		Message: "provider returned retryable unavailable",
	})
	if err != nil {
		t.Fatalf("Retry() error = %v, want nil", err)
	}
	if got, want := retry.Status, semanticqueue.StatusRetrying; got != want {
		t.Fatalf("retry.Status = %q, want %q", got, want)
	}
	if got, want := retry.AttemptCount, 1; got != want {
		t.Fatalf("retry.AttemptCount = %d, want %d", got, want)
	}
	if !retry.Retryable {
		t.Fatal("retry.Retryable = false, want true")
	}
	if retry.NextAttemptAt == nil || !retry.NextAttemptAt.Equal(next) {
		t.Fatalf("retry.NextAttemptAt = %v, want %v", retry.NextAttemptAt, next)
	}
	if got, want := retry.WorkItemID, job.WorkItemID; got != want {
		t.Fatalf("retry.WorkItemID = %q, want stable work item %q", got, want)
	}
	if got, want := retry.Fingerprint, job.Fingerprint; got != want {
		t.Fatalf("retry.Fingerprint = %q, want stable fingerprint %q", got, want)
	}

	dead, err := semanticqueue.DeadLetter(retry, now.Add(time.Minute), semanticqueue.Failure{
		Class:   semanticqueue.FailureClassRetryExhausted,
		Message: "retry budget exhausted",
	})
	if err != nil {
		t.Fatalf("DeadLetter() error = %v, want nil", err)
	}
	if got, want := dead.Status, semanticqueue.StatusDeadLetter; got != want {
		t.Fatalf("dead.Status = %q, want %q", got, want)
	}
	if dead.Retryable {
		t.Fatal("dead.Retryable = true, want terminal dead-letter record")
	}
	if dead.NextAttemptAt != nil {
		t.Fatalf("dead.NextAttemptAt = %v, want nil", dead.NextAttemptAt)
	}
	if got, want := dead.Failure.Class, semanticqueue.FailureClassRetryExhausted; got != want {
		t.Fatalf("dead.Failure.Class = %q, want %q", got, want)
	}
}

func TestRetryRejectsPastNextAttempt(t *testing.T) {
	t.Parallel()

	plan, err := semanticqueue.BuildPlan(basePlanRequest())
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}
	now := fixedTime().Add(time.Minute)
	_, err = semanticqueue.Retry(plan.Jobs[0], now, now.Add(-time.Second), semanticqueue.Failure{
		Class: semanticqueue.FailureClassProviderUnavailable,
	})
	if err == nil {
		t.Fatal("Retry() error = nil, want next-attempt validation error")
	}
}
