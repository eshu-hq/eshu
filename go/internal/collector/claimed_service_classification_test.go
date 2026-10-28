// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedServiceTerminalFailurePersistsStructuredClassification proves the
// #7926 round-trip: a Jira HTTP 400 terminal failure must persist its bounded
// operation, status code, failure class, and terminal flag on the claim
// mutation, so the next 400 is distinguishable in the database instead of
// collapsing to "jira provider failure: terminal".
func TestClaimedServiceTerminalFailurePersistsStructuredClassification(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
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
	httpErr := sdk.HTTPError{Provider: "jira", StatusCode: 400, Message: "Bad Request"}
	source := &stubClaimedSource{err: sdk.ClassifyProviderFailure("jira", httpErr, sdk.StatusPolicy{}, sdk.FailureRetryable)}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after terminal claim failure", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	got := store.lastTerminalFail
	if got.FailureClass != string(sdk.FailureTerminal) {
		t.Fatalf("FailureClass = %q, want %q", got.FailureClass, string(sdk.FailureTerminal))
	}
	if got.FailureOperation != "collect_failure" {
		t.Fatalf("FailureOperation = %q, want collect_failure", got.FailureOperation)
	}
	if got.FailureStatusCode != 400 {
		t.Fatalf("FailureStatusCode = %d, want 400", got.FailureStatusCode)
	}
	if !got.FailureTerminal {
		t.Fatal("FailureTerminal = false, want true for a terminal claim failure")
	}
	if got.FailureMessage == "" {
		t.Fatal("FailureMessage is empty, want the bounded provider message alongside the structured fields")
	}
	for _, leaked := range []string{"token", "secret", "https://"} {
		if strings.Contains(got.FailureMessage, leaked) || strings.Contains(got.FailureOperation, leaked) {
			t.Fatalf("structured failure leaks %q: message=%q operation=%q", leaked, got.FailureMessage, got.FailureOperation)
		}
	}
}

// TestClaimedServiceRetryableFailurePersistsStructuredClassification pins the
// retryable side of the same contract: a 429 keeps terminal false while still
// carrying its operation and status code.
func TestClaimedServiceRetryableFailurePersistsStructuredClassification(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 16, 12, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	httpErr := sdk.HTTPError{Provider: "jira", StatusCode: 429, Message: "Too Many Requests"}
	source := &stubClaimedSource{err: sdk.ClassifyProviderFailure("jira", httpErr, sdk.StatusPolicy{}, sdk.FailureRetryable)}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable claim failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	got := store.lastRetryableFail
	if got.FailureClass != string(sdk.FailureRateLimited) {
		t.Fatalf("FailureClass = %q, want %q", got.FailureClass, string(sdk.FailureRateLimited))
	}
	if got.FailureOperation != "collect_failure" {
		t.Fatalf("FailureOperation = %q, want collect_failure", got.FailureOperation)
	}
	if got.FailureStatusCode != 429 {
		t.Fatalf("FailureStatusCode = %d, want 429", got.FailureStatusCode)
	}
	if got.FailureTerminal {
		t.Fatal("FailureTerminal = true, want false for a retryable claim failure")
	}
}

// TestClaimedServiceBudgetExhaustedKeepsStageOperation is the P2-1 regression
// for PR #8254: a budget-exhausted claim whose cause carries no class must
// persist the claim stage token ("collect_failure") in the operation column
// while keeping "attempt_budget_exhausted" in the class column. Passing the
// budget class as the operation fallback leaks the class taxonomy into the
// operation column, mixing stage tokens with class names.
func TestClaimedServiceBudgetExhaustedKeepsStageOperation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 16, 12, 20, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 5
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
	source := &stubClaimedSource{err: errors.New("still throttled")}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after attempt budget routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	got := store.lastTerminalFail
	if got.FailureClass != FailureClassAttemptBudgetExhausted {
		t.Fatalf("FailureClass = %q, want %q", got.FailureClass, FailureClassAttemptBudgetExhausted)
	}
	if got.FailureOperation != "collect_failure" {
		t.Fatalf("FailureOperation = %q, want collect_failure stage token, not the budget class", got.FailureOperation)
	}
}

// TestClaimedServiceBudgetExhaustedKeepsCarriedClass pins the other dispatch
// variant of the same P2-1 fix: when the budget-exhausted cause already
// carries its own class, that class still wins over the budget fallback while
// the operation column keeps the stage token.
func TestClaimedServiceBudgetExhaustedKeepsCarriedClass(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 16, 12, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 5
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
	httpErr := sdk.HTTPError{Provider: "jira", StatusCode: 429, Message: "Too Many Requests"}
	source := &stubClaimedSource{err: sdk.ClassifyProviderFailure("jira", httpErr, sdk.StatusPolicy{}, sdk.FailureRetryable)}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after attempt budget routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	got := store.lastTerminalFail
	if got.FailureClass != string(sdk.FailureRateLimited) {
		t.Fatalf("FailureClass = %q, want %q", got.FailureClass, string(sdk.FailureRateLimited))
	}
	if got.FailureOperation != "collect_failure" {
		t.Fatalf("FailureOperation = %q, want collect_failure stage token", got.FailureOperation)
	}
	if got.FailureStatusCode != 429 {
		t.Fatalf("FailureStatusCode = %d, want 429", got.FailureStatusCode)
	}
}

// refinedClassFailure is a claim failure whose class is a collector-refined
// "<class>.<reason>" token, the shape an extracted collector reports through
// the extension host (eshu-hq/eshu#9289).
type refinedClassFailure struct{ class string }

func (e refinedClassFailure) Error() string {
	return "collector extension failure: " + e.class + ": extension requested terminal result"
}
func (e refinedClassFailure) FailureClass() string  { return e.class }
func (e refinedClassFailure) TerminalFailure() bool { return true }

// TestClaimedServiceTerminalFailurePersistsRefinedClassVerbatim pins that the
// dot in a refined failure class survives into the persisted claim mutation:
// the class column and message are not sanitized, only the operation token is.
// Extension collectors depend on this to surface a reason such as
// invalid_config.target_scope_mismatch in last_failure_class and
// last_failure_message without a host-side mapping.
func TestClaimedServiceTerminalFailurePersistsRefinedClassVerbatim(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
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
	const class = "invalid_config.target_scope_mismatch"
	source := &stubClaimedSource{err: refinedClassFailure{class: class}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after terminal claim failure", err)
	}
	got := store.lastTerminalFail
	if got.FailureClass != class {
		t.Fatalf("FailureClass = %q, want %q verbatim", got.FailureClass, class)
	}
	if !strings.Contains(got.FailureMessage, class) {
		t.Fatalf("FailureMessage = %q, want it to contain %q", got.FailureMessage, class)
	}
	if !got.FailureTerminal {
		t.Fatal("FailureTerminal = false, want true")
	}
}
