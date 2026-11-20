// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

// TestTimeoutClassLiteralMatchesOwningConstant binds the hardcoded class
// string above to reducer.HandlerTimeoutFailureClass without importing the
// reducer service package (which would cycle): it reads the owning source
// file and requires the declared constant value to equal the fake's class.
// Renaming the constant's value fails this test (#10944).
func TestTimeoutClassLiteralMatchesOwningConstant(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) = !ok, want caller file")
	}
	owner := filepath.Join(filepath.Dir(thisFile), "..", "..", "reducer", "service_handler_timeout.go")
	body, err := os.ReadFile(owner)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", owner, err)
	}
	const prefix = `HandlerTimeoutFailureClass = "`
	text := string(body)
	start := strings.Index(text, prefix)
	if start < 0 {
		t.Fatalf("owning constant declaration missing in %s", owner)
	}
	rest := text[start+len(prefix):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("owning constant value unterminated in %s", owner)
	}
	if got, want := (timeoutClassReducerTestError{}).FailureClass(), rest[:end]; got != want {
		t.Fatalf("test class %q != owning constant %q in %s: drift, update both", got, want, owner)
	}
}

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

	// #10879 phase 1 reports the terminal match count through
	// QueryContext, so the retry statement lands in queries.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[1], "reducer_handler_timeout"; got != want {
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

	// #10879 phase 1 reports the terminal match count through
	// QueryContext, so the dead-letter statement lands in queries.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "status = 'dead_letter'") {
		t.Fatalf("exhausted timeout should dead-letter, query:\n%s", db.queries[0].query)
	}
	// A self-classifying error wins over the triage fallback: the dead
	// letter keeps reducer_handler_timeout (not retry_exhausted), so the
	// operator sees that bound pressure — not generic transient pressure —
	// consumed the budget.
	if got, want := db.queries[0].args[1], "reducer_handler_timeout"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if details, _ := db.queries[0].args[3].(string); !strings.Contains(details, "disposition=retryable") {
		t.Fatalf("dead-letter details = %q, want disposition=retryable", details)
	}
}
