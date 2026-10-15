// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
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
				database:      db,
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
		database:      db,
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

// deliberatelyCountingReducerFailureClasses is the explicit exemption list for
// TestEveryReadinessFailureClassIsEnrolled: classes the reducer returns from a
// Retryable() readiness-shaped error that MUST count toward the retry budget,
// with the reason. They are never enrolled in
// nonCountingReducerRetryFailureClasses.
var deliberatelyCountingReducerFailureClasses = map[string]string{
	// #7597: the own deployment_mapping producer is dead-lettered, so the own
	// relationship generation can never activate. Deferring would wait
	// forever; counting dead-letters the consumer loudly, and replaying the
	// producer reopens it in the same statement (recovery.go wake).
	crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass: "own producer dead-lettered (#7597)",
	crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass:   "own producer dead-lettered (#7597)",
}

// TestDeliberatelyCountingClassesAreNeverEnrolled pins the exemption list
// against the class registry and against the class set the producer gate
// actually returns, so a class cannot drift into the non-counting set or out
// of the exemption list unnoticed.
func TestDeliberatelyCountingClassesAreNeverEnrolled(t *testing.T) {
	t.Parallel()

	for class, reason := range deliberatelyCountingReducerFailureClasses {
		if IsNonCountingReducerRetryFailureClass(class) {
			t.Fatalf("%q (%s) is enrolled as non-counting; it would retry forever on a dead producer", class, reason)
		}
	}
	for _, class := range crossrepo.ProducerDeadLetteredFailureClasses() {
		if _, ok := deliberatelyCountingReducerFailureClasses[class]; !ok {
			t.Fatalf("producer-terminal class %q is missing from the deliberate-counting exemption list", class)
		}
	}
	if got, want := len(crossrepo.ProducerDeadLetteredFailureClasses()), len(deliberatelyCountingReducerFailureClasses); got != want {
		t.Fatalf("producer gate returns %d classes, exemption list holds %d", got, want)
	}
	// The deferrals the same gate keeps returning stay non-counting.
	for _, class := range []string{
		reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		reducer.WorkloadMaterializationResolutionNotReadyFailureClass,
	} {
		if !IsNonCountingReducerRetryFailureClass(class) {
			t.Fatalf("own-gate deferral class %q is no longer non-counting", class)
		}
	}
}

// TestReducerQueueFailDeadLettersProducerTerminalClassesPastAttemptBudget is
// the per-class twin of the unenrolled control above: each producer-terminal
// class past MaxAttempts takes the dead-letter branch and keeps its class.
func TestReducerQueueFailDeadLettersProducerTerminalClassesPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 15, 11, 0, 0, 0, time.UTC)
	for class := range deliberatelyCountingReducerFailureClasses {
		t.Run(class, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{}
			queue := ReducerQueue{
				database: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute,
				RetryDelay: 2 * time.Minute, MaxAttempts: 3, Now: func() time.Time { return now },
			}
			intent := reducer.Intent{IntentID: "intent-" + class, AttemptCount: 3}
			if err := queue.Fail(context.Background(), intent, readinessMissError{class: class}); err != nil {
				t.Fatalf("Fail() error = %v, want nil", err)
			}
			if got, want := len(db.execs), 1; got != want {
				t.Fatalf("exec count = %d, want %d", got, want)
			}
			if query := db.execs[0].query; strings.Contains(query, "status = 'retrying'") {
				t.Fatalf("producer-terminal class %q past the budget was deferred:\n%s", class, query)
			}
			if got := db.execs[0].args[1]; got != class {
				t.Fatalf("dead-letter failure class = %v, want %q", got, class)
			}
		})
	}
}
