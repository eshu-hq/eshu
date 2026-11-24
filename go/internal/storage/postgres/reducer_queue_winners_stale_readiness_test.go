// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerQueueFailParksWinnersWatermarkStaleOnRetryableBudget pins the
// retry-loop fix for vulnerability-intelligence advisory waits: a stale
// winners-watermark wait inside the attempt budget must park on the
// winners_watermark wait kind (10-minute backstop + in-transaction resweep
// wake) instead of churning on the short exponential backoff. Before the fix
// only the not_ready class had a WaitingDeferral entry, so stale Fail took
// the timer path: every stale row re-claimed every few seconds, starving the
// supply_chain_impact drain while the watermark was stuck behind a long
// resweep, then burned its 3 counting attempts and poisoned as dead_letter.
// Parked stale rows wait for the resweep commit wake (reopened_at = covering
// watermark, so the next claim passes at once) or the backstop, and still
// dead-letter past MaxAttempts when the maintainer is truly down (see
// TestReducerQueueFailDeadLettersWinnersWatermarkStalePastAttemptBudget).
func TestReducerQueueFailParksWinnersWatermarkStaleOnRetryableBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
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
		IntentID:       "intent-stale-park",
		ScopeID:        "synthetic:advisory-groups",
		GenerationID:   "synthetic:advisory-groups:genesis",
		EnqueuedAt:     now.Add(-time.Hour),
		CycleStartedAt: now.Add(-time.Hour),
		AttemptCount:   1,
	}

	if err := queue.Fail(context.Background(), intent, winnersWatermarkStaleTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	// The retry statement lands in queries while the wait-row park stays an
	// exec; the park then re-checks satisfaction, so the Fail issues two
	// queries and one exec in total.
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
	if got, want := db.queries[0].args[1], advisorygroup.WinnersWatermarkStaleFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	// A parked stale wait returns on the 10-minute backstop, not on the
	// exponential backoff: the wait row carries the waits-for and the
	// resweep-commit wake reopens it, so the backoff only bounds a wait the
	// map does not park.
	if got, want := db.queries[0].args[4], now.Add(WaitingBackstopDelay); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
	// The park writes exactly one wait row for the deferred intent.
	if got := db.execs[0].query; !strings.Contains(got, "INSERT INTO reducer_waiting_items") {
		t.Fatalf("exec is not the wait-row park: %s", got)
	}
}

// TestReducerQueueFailDeadLettersWinnersWatermarkStalePastAttemptBudget pins
// the counting half of the contract: parking the stale wait must not hide a
// dead maintainer. Past MaxAttempts the stale Fail still takes the
// dead-letter branch and never parks, so a watermark stuck for hours
// surfaces visibly instead of waiting in silence.
func TestReducerQueueFailDeadLettersWinnersWatermarkStalePastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
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
		IntentID:       "intent-stale-dead",
		ScopeID:        "synthetic:advisory-groups",
		GenerationID:   "synthetic:advisory-groups:genesis",
		EnqueuedAt:     now.Add(-2 * time.Hour),
		CycleStartedAt: now.Add(-2 * time.Hour),
		AttemptCount:   42,
	}

	if err := queue.Fail(context.Background(), intent, winnersWatermarkStaleTestError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	// The dead-letter statement lands in queries and no wait row is parked.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := db.queries[0].query; strings.Contains(got, "status = 'retrying'") {
		t.Fatalf("a stale class past the attempt budget was deferred; the park must not exempt a counting class:\n%s", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0 (no park, no own-gate wake for the stale class)", got)
	}
}

type winnersWatermarkStaleTestError struct{}

func (winnersWatermarkStaleTestError) Error() string {
	return "advisory group projection waiting for supply chain impact winners rebuilt at 2026-11-24T15:40:25Z to reach the intent enqueue time 2026-11-24T17:31:23Z"
}

func (winnersWatermarkStaleTestError) Retryable() bool {
	return true
}

func (winnersWatermarkStaleTestError) FailureClass() string {
	return advisorygroup.WinnersWatermarkStaleFailureClass
}
