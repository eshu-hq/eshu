// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestFailRetryableTerminalsWhenBudgetExhausted pins the Track B1 choke
// point (eshu-hq/eshu#10791): every retryable failure path funnels through
// failRetryable, so the attempt-budget check belongs inside it. At an
// exhausted budget the failure must go terminal with class
// "attempt_budget_exhausted" instead of requeueing for another stale retry.
func TestFailRetryableTerminalsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 3
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{item: item, claim: claim, found: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	mutation := workflow.ClaimMutation{
		WorkItemID: item.WorkItemID,
		ClaimID:    claim.ClaimID,
		ObservedAt: now,
	}
	if err := service.failRetryable(ctx, mutation, item, "collect_failure", errors.New("still throttled")); err == nil {
		t.Fatal("failRetryable error = nil, want the retryable-recorded sentinel")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d (budget exhausted must go terminal)", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastTerminalFail.FailureClass, "attempt_budget_exhausted"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

// TestFailRetryablePinsBudgetClassForClassifiedCauses pins the persisted
// class on the budget choke point (eshu-hq/eshu#10791 review P1): a
// classified provider cause (rate-limited here) must still terminal as
// "attempt_budget_exhausted", not keep the cause's own class, so dashboards
// filtering on the budget class do not undercount.
func TestFailRetryablePinsBudgetClassForClassifiedCauses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 3
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{item: item, claim: claim, found: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	mutation := workflow.ClaimMutation{
		WorkItemID: item.WorkItemID,
		ClaimID:    claim.ClaimID,
		ObservedAt: now,
	}
	if err := service.failRetryable(ctx, mutation, item, "collect_failure", rateLimitedCollectFailure{}); err == nil {
		t.Fatal("failRetryable error = nil, want the terminal-recorded sentinel")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d (budget exhausted must go terminal)", got, want)
	}
	if got, want := store.lastTerminalFail.FailureClass, "attempt_budget_exhausted"; got != want {
		t.Fatalf("FailureClass = %q, want %q (budget class pins independent of cause)", got, want)
	}
}

// TestCommitPathPinsBudgetClassForClassifiedCauses pins the persisted class
// on the commit-path outer budget check (eshu-hq/eshu#10804 review P1): a
// classified provider commit failure on an exhausted budget must terminal as
// "attempt_budget_exhausted", not keep the cause's own class.
func TestCommitPathPinsBudgetClassForClassifiedCauses(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 3
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true}
	committer := &stubClaimedCommitter{
		claimedCommit: func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
			return rateLimitedCollectFailure{}
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	service.MaxAttempts = 3
	service.HeartbeatInterval = 30 * time.Second

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after commit failure routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d (budget exhausted must go terminal)", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastTerminalFail.FailureClass, "attempt_budget_exhausted"; got != want {
		t.Fatalf("FailureClass = %q, want %q (budget class pins independent of cause)", got, want)
	}
}

// TestFailRetryableRequeuesWithinBudget guards the choke point against
// overreach: an in-budget item keeps the existing retryable shape.
func TestFailRetryableRequeuesWithinBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 2
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{item: item, claim: claim, found: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	mutation := workflow.ClaimMutation{
		WorkItemID: item.WorkItemID,
		ClaimID:    claim.ClaimID,
		ObservedAt: now,
	}
	if err := service.failRetryable(ctx, mutation, item, "collect_failure", errors.New("still throttled")); err == nil {
		t.Fatal("failRetryable error = nil, want the retryable-recorded sentinel")
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.terminalFailCalls, 0; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
}

// TestDispatchLeaseFailureTerminalsWhenBudgetExhausted covers the H1 hole
// at the dispatch heartbeat (grantDispatchLease): the dispatch site mirrors
// the collect/commit outer budget checks, so an exhausted item terminals
// instead of requeueing. The run outcome must read fail_terminal, not
// fail_retryable, so the duration metric and the claimed_run span agree with
// the row (eshu-hq/eshu#10791 review F1).
func TestDispatchLeaseFailureTerminalsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 3
	claim := testWorkflowClaim(item.WorkItemID, now)
	heartbeats := 0
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			heartbeats++
			if heartbeats > 1 {
				return errors.New("dispatch heartbeat lost")
			}
			return nil
		},
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	instruments, metricReader := newTestInstrumentsPair(t)
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})
	service.MaxAttempts = 3
	service.HeartbeatInterval = 30 * time.Second
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after dispatch failure routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got, want := claimedHistogramOutcome(t, rm, "eshu_dp_workflow_claim_run_duration_seconds"), telemetry.CollectorRunOutcomeFailTerminal; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
}

// TestReleasePathTerminalsWhenBudgetExhausted covers the H1 hole on the
// release path (NextClaimed ok=false): an exhausted item terminals here
// instead of releasing back to pending, which reset nothing and burned no
// budget visibly.
func TestReleasePathTerminalsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.November, 16, 17, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 3
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{ok: false}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 3
	service.HeartbeatInterval = 30 * time.Second

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after release path routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.releaseCalls, 0; got != want {
		t.Fatalf("release calls = %d, want %d (exhausted item must not release)", got, want)
	}
}

// TestClaimRetryDelayCappedAtOneHour pins the Track B1 visibility cap: an
// unbounded provider Retry-After must not push one attempt's invisibility
// past an hour, or 10 attempts can span days.
func TestClaimRetryDelayCappedAtOneHour(t *testing.T) {
	t.Parallel()

	if got, want := ClaimRetryDelay(30*time.Second, 72*time.Hour), time.Hour; got != want {
		t.Fatalf("ClaimRetryDelay(30s, 72h) = %v, want %v", got, want)
	}
	if got, want := ClaimRetryDelay(30*time.Second, 5*time.Minute), 5*time.Minute; got != want {
		t.Fatalf("ClaimRetryDelay(30s, 5m) = %v, want %v (below-cap delays pass through)", got, want)
	}
	if got, want := ClaimRetryDelay(30*time.Second, 0), 30*time.Second; got != want {
		t.Fatalf("ClaimRetryDelay(30s, 0) = %v, want %v (poll interval floor holds)", got, want)
	}
}
