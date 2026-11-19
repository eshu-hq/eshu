// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// This file proves the queue behavior for the reducer handler-timeout class
// (#10866): an expired per-domain bound fails the intent through the
// existing WorkSink.Fail triage with the counting reducer_handler_timeout
// class, so the item retries under the normal attempt budget and
// dead-letters with the retry_exhausted triage when the budget is spent.

// timeoutClassReducerTestError is a retryable self-classified error carrying
// the reducer handler-timeout class. The class string is spelled out here so
// this package proves the queue behavior for it without importing the
// reducer service (an import cycle: postgres tests already import the reducer
// package). The owning constant is
// reducer.HandlerTimeoutFailureClass in
// go/internal/reducer/service_handler_timeout.go; the two must stay
// byte-identical.
type timeoutClassReducerTestError struct {
	message string
}

func (e timeoutClassReducerTestError) Error() string { return e.message }

func (e timeoutClassReducerTestError) Retryable() bool { return true }

func (e timeoutClassReducerTestError) FailureClass() string { return "reducer_handler_timeout" }

// TestReducerQueueFailRetriesTimeoutClassWithinAttemptBudget proves a
// timeout-class failure retries with its class preserved on the retrying
// row, so the operator can scope the retry surge to bound pressure and the
// ready row re-enters the normal claim poll (criterion 4, repeat-timeout
// fate: retry, never on-the-spot dead-letter).
func TestReducerQueueFailRetriesTimeoutClassWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
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
		IntentID:     "intent-timeout-1",
		AttemptCount: 1,
	}

	if err := queue.Fail(context.Background(), intent, timeoutClassReducerTestError{message: "bound expired"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], "reducer_handler_timeout"; got != want {
		t.Fatalf("failure class = %v, want %v: the timeout class must survive the retry row", got, want)
	}
}

// TestReducerQueueFailDeadLettersTimeoutClassWhenBudgetExhausted proves the
// repeat-timeout fate at the attempt boundary: a timeout-class failure on
// its last budgeted attempt dead-letters with the retry_exhausted triage
// class and a retryable disposition, exactly like any other counting
// transient — the timeout needs no queue-code change (criterion 4).
func TestReducerQueueFailDeadLettersTimeoutClassWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   2,
		Now:           func() time.Time { return now },
	}

	intent := reducercontract.Intent{
		IntentID:     "intent-timeout-1",
		AttemptCount: 2,
	}

	if err := queue.Fail(context.Background(), intent, timeoutClassReducerTestError{message: "bound expired again"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("exhausted timeout should dead-letter, query:\n%s", db.execs[0].query)
	}
	// A self-classifying error wins over the triage fallback: the dead
	// letter keeps reducer_handler_timeout (not retry_exhausted), so the
	// operator sees that bound pressure — not generic transient pressure —
	// consumed the budget.
	if got, want := db.execs[0].args[1], "reducer_handler_timeout"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if details, _ := db.execs[0].args[3].(string); !strings.Contains(details, "disposition=retryable") {
		t.Fatalf("dead-letter details = %q, want disposition=retryable", details)
	}
}
