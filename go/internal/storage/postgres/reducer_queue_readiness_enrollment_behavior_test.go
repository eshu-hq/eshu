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

// readinessMissError stands in for any of the reducer's in-handler
// readiness-gate errors: retryable, self-classifying, and carrying the class
// under test. Every one of them has this exact shape (a Retryable() returning
// true plus a FailureClass() returning the class), which is what the go/ast
// guard in reducer_queue_readiness_enrollment_test.go keys on.
type readinessMissError struct{ class string }

func (e readinessMissError) Error() string        { return e.class }
func (readinessMissError) Retryable() bool        { return true }
func (e readinessMissError) FailureClass() string { return e.class }

// TestReducerQueueFailDefersEveryEnrolledReadinessClassPastAttemptBudget is the
// behavioral half of #5046: the go/ast guard proves each readiness class is
// REGISTERED, this proves registration actually defers the row.
//
// It drives the real ReducerQueue.Fail path for every enrolled class with an
// AttemptCount far past MaxAttempts, and requires a 'retrying' UPDATE rather
// than the dead-letter UPDATE. Before the enrollment, the seventeen classes
// added by #5046 took the dead-letter branch here — a still-pending intent
// permanently parked in a state the succeeded-only reopen path never reopens.
//
// Table-driven over the registry rather than one test per class on purpose: a
// hand-written per-class test is exactly the pattern that let the registry
// drift in the first place, since adding a class does not add its test.
func TestReducerQueueFailDefersEveryEnrolledReadinessClassPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 11, 0, 0, 0, time.UTC)

	for _, class := range nonCountingReducerRetryFailureClasses {
		t.Run(class, func(t *testing.T) {
			t.Parallel()

			db := &fakeExecQueryer{}
			queue := ReducerQueue{
				db:            db,
				LeaseOwner:    "reducer-1",
				LeaseDuration: time.Minute,
				RetryDelay:    2 * time.Minute,
				MaxAttempts:   3,
				Now:           func() time.Time { return now },
			}

			intent := reducer.Intent{IntentID: "intent-" + class, AttemptCount: 42}
			if err := queue.Fail(context.Background(), intent, readinessMissError{class: class}); err != nil {
				t.Fatalf("Fail() error = %v, want nil", err)
			}
			if got, want := len(db.execs), 1; got != want {
				t.Fatalf("exec count = %d, want %d", got, want)
			}

			query := db.execs[0].query
			if !strings.Contains(query, "status = 'retrying'") {
				t.Fatalf(
					"AttemptCount=42 with an enrolled readiness class took a non-retrying branch; "+
						"the class is registered but the row is not deferred:\n%s",
					query,
				)
			}
			if strings.Contains(query, "'dead_letter'") {
				t.Fatalf("readiness miss dead-lettered despite enrollment:\n%s", query)
			}
			if got, want := db.execs[0].args[1], class; got != want {
				t.Fatalf("failure class = %v, want %v", got, want)
			}
		})
	}
}

// TestReducerQueueFailDeadLettersAnUnenrolledClassPastAttemptBudget is the
// negative control. Without it the test above could pass because Fail defers
// everything, which would prove nothing about enrollment.
func TestReducerQueueFailDeadLettersAnUnenrolledClassPastAttemptBudget(t *testing.T) {
	t.Parallel()

	const unenrolled = "definitely_not_enrolled_nodes_not_ready"
	for _, class := range nonCountingReducerRetryFailureClasses {
		if class == unenrolled {
			t.Fatalf("%q is enrolled; pick a class that is not, or this control proves nothing", unenrolled)
		}
	}

	now := time.Date(2026, time.August, 9, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{IntentID: "intent-control", AttemptCount: 42}
	if err := queue.Fail(context.Background(), intent, readinessMissError{class: unenrolled}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if query := db.execs[0].query; strings.Contains(query, "status = 'retrying'") {
		t.Fatalf(
			"an UNENROLLED class past the attempt budget was deferred; enrollment is not what "+
				"decides deferral, so the enrollment test above proves nothing:\n%s",
			query,
		)
	}
}
