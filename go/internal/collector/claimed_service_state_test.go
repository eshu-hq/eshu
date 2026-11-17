// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// stateCommitter is a claim-aware committer that also persists collector-owned
// generation state, the way the Postgres ingestion store does.
type stateCommitter struct {
	stubClaimedCommitter
	stateCalls  int
	wroteState  bool
	stateWriter GenerationStateWriter
}

type discardExecutor struct{}

func (discardExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (c *stateCommitter) CommitClaimedScopeGenerationWithState(
	ctx context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	_ func() error,
	writer GenerationStateWriter,
) error {
	c.stateCalls++
	c.stateWriter = writer
	for range factStream {
	}
	if err := writer(ctx, discardExecutor{}); err != nil {
		return err
	}
	c.wroteState = true
	return nil
}

func runStateClaim(t *testing.T, committer Committer, writer GenerationStateWriter) (*stubClaimStore, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item: item, claim: claim, found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error { cancel(); return nil },
	}
	collected := FactsFromSlice(testScope(), testGeneration(now), testFacts(now))
	collected.StateWriter = writer
	source := &stubClaimedSource{collected: collected, ok: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	return store, service.Run(ctx)
}

func TestClaimedServiceCommitsGenerationStateThroughStateCommitter(t *testing.T) {
	t.Parallel()

	wrote := false
	committer := &stateCommitter{}
	store, err := runStateClaim(t, committer, func(context.Context, CommitExecutor) error {
		wrote = true
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if committer.stateCalls != 1 || !committer.wroteState || !wrote {
		t.Fatalf("state commit calls = %d, wrote = %v/%v, want one call that ran the writer", committer.stateCalls, committer.wroteState, wrote)
	}
	if committer.claimedCalls != 0 || committer.claimedStreamErrorCalls != 0 {
		t.Errorf("plain commit calls = %d/%d, want 0: the state must travel with the generation", committer.claimedCalls, committer.claimedStreamErrorCalls)
	}
	if store.completeCalls != 1 {
		t.Errorf("complete calls = %d, want 1", store.completeCalls)
	}
}

func TestClaimedServiceFailsClaimWhenCommitterCannotPersistGenerationState(t *testing.T) {
	t.Parallel()

	committer := &stubClaimedCommitter{}
	store, err := runStateClaim(t, committer, func(context.Context, CommitExecutor) error { return nil })
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: a commit failure is recorded on the claim", err)
	}
	if committer.claimedCalls != 0 {
		t.Fatalf("plain commit calls = %d, want 0: committing without the state would hide a truncated read", committer.claimedCalls)
	}
	if store.completeCalls != 0 {
		t.Errorf("complete calls = %d, want 0", store.completeCalls)
	}
	if store.retryableFailCalls+store.terminalFailCalls != 1 {
		t.Fatalf("claim failures = %d retryable, %d terminal, want exactly one", store.retryableFailCalls, store.terminalFailCalls)
	}
	failure := store.lastRetryableFail
	if store.terminalFailCalls == 1 {
		failure = store.lastTerminalFail
	}
	if !strings.Contains(failure.FailureClass, "commit_failure") {
		t.Errorf("failure class = %q, want commit_failure", failure.FailureClass)
	}
}

// classedStateError carries its own failure class the way the NVD baseline
// seed rollback error does (storage vulnerability, eshu-hq/eshu#10810).
type classedStateError struct{}

func (classedStateError) Error() string        { return "state writer refused" }
func (classedStateError) FailureClass() string { return "nvd_baseline_seed_rollback" }

// TestClaimedServiceRecordsTheStateWriterFailureClass proves a writer error
// that carries its own class reaches the claim failure row with that class
// instead of the generic commit_failure, through the wrapping the commit path
// applies, and the claim fails retryable so the next plan re-mints the chunk.
func TestClaimedServiceRecordsTheStateWriterFailureClass(t *testing.T) {
	t.Parallel()

	store, err := runStateClaim(t, &stateCommitter{}, func(context.Context, CommitExecutor) error {
		return fmt.Errorf("write collector generation state: %w", classedStateError{})
	})
	if err != nil && !errors.Is(err, errRetryableClaimRecorded) {
		t.Fatalf("Run() error = %v, want nil or the recorded-retry sentinel", err)
	}
	if store.terminalFailCalls != 0 || store.retryableFailCalls != 1 {
		t.Fatalf("claim failures = %d retryable, %d terminal, want one retryable", store.retryableFailCalls, store.terminalFailCalls)
	}
	if got := store.lastRetryableFail.FailureClass; got != "nvd_baseline_seed_rollback" {
		t.Fatalf("failure class = %q, want nvd_baseline_seed_rollback", got)
	}
	if store.completeCalls != 0 {
		t.Fatalf("complete calls = %d, want 0: a rolled-back generation never completes the claim", store.completeCalls)
	}
}

func TestClaimedServiceWithoutGenerationStateKeepsPlainCommit(t *testing.T) {
	t.Parallel()

	committer := &stateCommitter{}
	_, err := runStateClaim(t, committer, nil)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if committer.stateCalls != 0 || committer.claimedCalls != 1 {
		t.Fatalf("state calls = %d, plain calls = %d, want 0 and 1", committer.stateCalls, committer.claimedCalls)
	}
}

// TestServiceRefusesGenerationStateItCannotPersist proves the unclaimed Service
// fails closed on a generation that carries a state writer: only ClaimedService
// hands the writer to a committer that runs it in the commit transaction, so
// committing the facts alone would drop the state without an error.
func TestServiceRefusesGenerationStateItCannotPersist(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	committer := &stubCommitter{}
	service := Service{Committer: committer, PollInterval: time.Millisecond}
	collected := FactsFromSlice(testScope(), testGeneration(now), testFacts(now))
	collected.StateWriter = func(context.Context, CommitExecutor) error { return nil }

	err := service.commitWithTelemetry(context.Background(), collected, time.Now())
	if err == nil || !strings.Contains(err.Error(), "generation state") {
		t.Fatalf("commitWithTelemetry() error = %v, want a refusal that names generation state", err)
	}
	if committer.calls != 0 {
		t.Errorf("CommitScopeGeneration() calls = %d, want 0: the facts must not commit without their state", committer.calls)
	}
}
