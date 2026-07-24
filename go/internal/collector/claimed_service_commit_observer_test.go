// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// stubClaimedSourceWithObserver wraps stubClaimedSource with an optional
// ClaimedGenerationCommitObserver implementation, so tests can assert
// exactly when (and how often) ClaimedService invokes the post-commit hook
// relative to the commit itself (#5429).
type stubClaimedSourceWithObserver struct {
	*stubClaimedSource
	observeCalls int
	lastObserved workflow.WorkItem
	observeErr   error
}

func (s *stubClaimedSourceWithObserver) ObserveClaimedGenerationCommitted(
	_ context.Context,
	item workflow.WorkItem,
) error {
	s.observeCalls++
	s.lastObserved = item
	return s.observeErr
}

// TestClaimedServiceObservesCommitOnlyAfterSuccessfulCommit pins the #5429
// ordering contract at the collector.ClaimedService level: a
// ClaimedGenerationCommitObserver must be invoked exactly once, with the
// SAME work item NextClaimed received, and only once the commit has
// durably succeeded (proven here by requiring commitCalls == 1 before the
// observer call is asserted).
func TestClaimedServiceObservesCommitOnlyAfterSuccessfulCommit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSourceWithObserver{
		stubClaimedSource: &stubClaimedSource{
			collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
			ok:        true,
		},
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("commit calls = %d, want %d", got, want)
	}
	if got, want := source.observeCalls, 1; got != want {
		t.Fatalf("observe calls = %d, want %d", got, want)
	}
	if got := source.lastObserved.WorkItemID; got != item.WorkItemID {
		t.Fatalf("observed work item = %q, want %q", got, item.WorkItemID)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
}

// TestClaimedServiceDoesNotObserveOnFailedCommit proves the other half of
// the #5429 ordering contract: when commitCollected fails, the observer must
// NOT be called. A source that only advances its progress marker inside the
// observer hook (as ghactionsruntime now does) must never see that hook
// fire for a generation whose facts never committed.
func TestClaimedServiceDoesNotObserveOnFailedCommit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 1, 12, 5, 0, 0, time.UTC)
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
	source := &stubClaimedSourceWithObserver{
		stubClaimedSource: &stubClaimedSource{
			collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
			ok:        true,
		},
	}
	wantErr := errors.New("commit failed")
	committer := &stubClaimedCommitter{
		claimedCommit: func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
			return wantErr
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable commit failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := source.observeCalls, 0; got != want {
		t.Fatalf("observe calls = %d, want %d (must not fire on failed commit)", got, want)
	}
}

// TestClaimedServiceCompletesClaimWhenObserverErrors proves the observer's
// error-handling contract: a hook error is non-fatal. The facts already
// committed durably, so ClaimedService must still complete the claim
// normally rather than failing it or rolling back the commit.
func TestClaimedServiceCompletesClaimWhenObserverErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 1, 12, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSourceWithObserver{
		stubClaimedSource: &stubClaimedSource{
			collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
			ok:        true,
		},
		observeErr: errors.New("watermark store unavailable"),
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil even when the commit observer errors", err)
	}
	if got, want := source.observeCalls, 1; got != want {
		t.Fatalf("observe calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d (observer error must not block completion)", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.terminalFailCalls, 0; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
}
