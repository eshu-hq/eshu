// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// This file holds the core ReducerQueue enqueue/claim/fail unit tests. It was
// split out of work_queue_test.go (which now holds only ProjectorQueue tests)
// to keep both files under the repo's 500-line cap after the #4450
// exponential-backoff-with-jitter change added assertions and comments to
// several retry tests.

func TestReducerQueueEnqueueAndClaimRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"reducer_scope-123_generation-456_workload_identity_repo-123_fact-1_20260412110000.000000000_0",
					"scope-123",
					"generation-456",
					"workload_identity",
					1,
					now,
					now,
					[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
				}},
			},
		},
	}

	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	result, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Domain:       "workload_identity",
		EntityKey:    "repo-123",
		Reason:       "shared follow-up",
		FactID:       "fact-1",
		SourceSystem: "git",
	}})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := result.Count, 1; got != want {
		t.Fatalf("Enqueue().Count = %d, want %d", got, want)
	}

	intent, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainWorkloadIdentity; got != want {
		t.Fatalf("Claim().Domain = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("Claim().SourceSystem = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys[0], "repo-123"; got != want {
		t.Fatalf("Claim().EntityKeys[0] = %q, want %q", got, want)
	}
	if got, want := intent.AttemptCount, 1; got != want {
		t.Fatalf("Claim().AttemptCount = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query = %q, want fact_work_items insert", db.execs[0].query)
	}
}

func TestReducerQueueEnqueueRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	_, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Domain:       "not_a_real_domain",
		EntityKey:    "repo-123",
		Reason:       "shared follow-up",
		FactID:       "fact-1",
		SourceSystem: "git",
	}})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want non-nil")
	}
}

func TestScanReducerIntentRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	rows := &queueFakeRows{
		rows: [][]any{{
			"intent-1",
			"scope-123",
			"generation-456",
			"not_a_real_domain",
			1,
			time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
			time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
			[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
		}},
	}

	if _, err := scanReducerIntent(rows); err == nil {
		t.Fatal("scanReducerIntent() error = nil, want non-nil")
	}
}

func TestReducerQueueFailRetriesRetryableErrorWithinAttemptBudget(t *testing.T) {
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
		AttemptCount: 1,
	}

	if err := queue.Fail(context.Background(), intent, retryableReducerTestError{message: "transient failure"}); err != nil {
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
			t.Fatalf("retry query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], "reducer_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	// Exponential backoff (#4450): AttemptCount=1 doubles the 2m base delay to
	// 4m (baseDelay*(1<<attempt)); JitterFraction is unset (0) so this stays
	// deterministic with no added jitter term.
	if got, want := db.execs[0].args[4], now.Add(4*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

func TestReducerQueueFailMarksRetryableErrorTerminalWhenAttemptBudgetExhausted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   2,
		Now:           func() time.Time { return now },
	}

	intent := reducer.Intent{
		IntentID:     "intent-1",
		AttemptCount: 2,
	}

	if err := queue.Fail(context.Background(), intent, retryableReducerTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'dead_letter'",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("terminal query missing %q:\n%s", want, query)
		}
	}
	// A retryable cause that exhausted its budget is dead-lettered as the
	// retry_exhausted triage class (the transient bucket) so an operator can tell
	// it gave up under transient pressure rather than from a terminal defect.
	if got, want := db.execs[0].args[1], string(projector.TriageClassRetryExhausted); got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if details, _ := db.execs[0].args[3].(string); !strings.Contains(details, "disposition=retryable") {
		t.Fatalf("dead-letter details = %q, want disposition=retryable", details)
	}
}

// TestReducerQueueFailDeadLettersTerminalWithTriageClass proves a non-retryable
// reducer failure is dead-lettered with an operator-facing triage class
// (projection_bug for an unclassified terminal error) instead of the coarse
// "reducer_failed" fallback — the issue #3502 dead-letter-triage surface on the
// reducer queue.
func TestReducerQueueFailDeadLettersTerminalWithTriageClass(t *testing.T) {
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

	intent := reducer.Intent{IntentID: "intent-1"}

	if err := queue.Fail(context.Background(), intent, errors.New("nil deref in materializer")); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("terminal error should dead-letter, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], string(projector.TriageClassProjectionBug); got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if details, _ := db.execs[0].args[3].(string); !strings.Contains(details, "disposition=manual_review") {
		t.Fatalf("dead-letter details = %q, want disposition=manual_review", details)
	}
}

func TestReducerQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget(t *testing.T) {
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
		AttemptCount: 1,
	}
	cause := fmt.Errorf("semantic materialization: %w", sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "semantic label=Annotation rows=10",
		Cause:       context.DeadlineExceeded,
	})

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("timeout should retry within attempt budget, query:\n%s", db.execs[0].query)
	}
	// A graph-write timeout retry preserves its self-classified graph_write_timeout
	// failure class on the retrying row (instead of the generic reducer_retryable)
	// so producer write-timeout backpressure (#3560) can scope its pressure signal
	// to graph-write classes and never throttle on a reducer readiness backlog.
	if got, want := db.execs[0].args[1], "graph_write_timeout"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "semantic label=Annotation rows=10"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
	// Exponential backoff (#4450): AttemptCount=1 doubles the 2m base delay to
	// 4m (baseDelay*(1<<attempt)); JitterFraction is unset (0) so this stays
	// deterministic with no added jitter term.
	if got, want := db.execs[0].args[4], now.Add(4*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}

// TestReducerQueueFailRetriesReadinessBacklogKeepsOwnFailureClass proves a
// reducer readiness backlog (a *_not_ready cross-scope readiness miss) keeps its
// own non-graph failure class on the retrying row and is NOT relabeled as a
// graph-write class. This is the regression guard for #3560: producer
// write-timeout backpressure must scope its pressure signal to graph-write
// classes only, so a readiness backlog can never be mistaken for graph-write
// pressure and throttle unrelated admission.
func TestReducerQueueFailRetriesReadinessBacklogKeepsOwnFailureClass(t *testing.T) {
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
	intent := reducer.Intent{IntentID: "intent-1", AttemptCount: 1}

	if err := queue.Fail(context.Background(), intent, readinessBacklogTestError{
		class: reducer.SecretsIAMEndpointNotReadyFailureClass,
	}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("readiness backlog should retry, query:\n%s", db.execs[0].query)
	}
	if got := db.execs[0].args[1]; got != reducer.SecretsIAMEndpointNotReadyFailureClass {
		t.Fatalf("failure class = %v, want %v", got, reducer.SecretsIAMEndpointNotReadyFailureClass)
	}
	if got := db.execs[0].args[1]; got == sourcecypher.GraphWriteTimeoutFailureClass {
		t.Fatalf("readiness backlog must not be labeled a graph-write class, got %v", got)
	}
}

// readinessBacklogTestError is a retryable, self-classifying reducer readiness
// miss used to prove readiness backlogs keep their own failure class.
type readinessBacklogTestError struct {
	class string
}

func (e readinessBacklogTestError) Error() string        { return e.class }
func (readinessBacklogTestError) Retryable() bool        { return true }
func (e readinessBacklogTestError) FailureClass() string { return e.class }
