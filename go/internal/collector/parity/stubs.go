// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TerminalError marks a wrapped error as a terminal collector failure so the
// claim routes to FailClaimTerminal instead of FailClaimRetryable. A plain error
// is retryable. It implements the collector terminal-failure contract.
type TerminalError struct {
	// Err is the underlying cause.
	Err error
	// Class optionally overrides the recorded failure class.
	Class string
}

// Error returns the underlying cause message.
func (e TerminalError) Error() string { return e.Err.Error() }

// Unwrap exposes the wrapped error.
func (e TerminalError) Unwrap() error { return e.Err }

// TerminalFailure reports that this failure must not be retried.
func (e TerminalError) TerminalFailure() bool { return true }

// FailureClass returns the optional override failure class.
func (e TerminalError) FailureClass() string { return e.Class }

// recordingControlStore serves one claimed work item, records the terminal claim
// disposition, and cancels the run context once the work item is exhausted so
// the collector's poll loop stops deterministically.
type recordingControlStore struct {
	item         workflow.WorkItem
	claim        workflow.Claim
	cancel       context.CancelFunc
	served       bool
	outcome      ClaimOutcome
	failureClass string
}

func (s *recordingControlStore) ClaimNextEligible(
	_ context.Context,
	_ workflow.ClaimSelector,
	_ time.Time,
	_ time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.served {
		if s.cancel != nil {
			s.cancel()
		}
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.served = true
	return s.item, s.claim, true, nil
}

func (s *recordingControlStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *recordingControlStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.outcome = ClaimCompleted
	return nil
}

func (s *recordingControlStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	s.outcome = ClaimReleased
	return nil
}

func (s *recordingControlStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.outcome = ClaimFailedRetryable
	s.failureClass = mutation.FailureClass
	return nil
}

func (s *recordingControlStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.outcome = ClaimFailedTerminal
	s.failureClass = mutation.FailureClass
	return nil
}

// fixtureSource returns one pre-built collected generation (or an error) for a
// claimed work item.
type fixtureSource struct {
	generation collector.CollectedGeneration
	ok         bool
	err        error
}

func (s *fixtureSource) NextClaimed(context.Context, workflow.WorkItem) (collector.CollectedGeneration, bool, error) {
	return s.generation, s.ok, s.err
}

// recordingCommitter records committed facts in memory and can fail the commit
// to exercise the dead-letter and retry paths. A failed commit records no facts,
// mirroring the atomic durable boundary.
type recordingCommitter struct {
	committed []facts.Envelope
	err       error
}

func (c *recordingCommitter) CommitScopeGeneration(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	return c.commit(ctx, stream)
}

func (c *recordingCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	return c.commit(ctx, stream)
}

func (c *recordingCommitter) commit(_ context.Context, stream <-chan facts.Envelope) error {
	drained := make([]facts.Envelope, 0)
	for envelope := range stream {
		drained = append(drained, envelope)
	}
	if c.err != nil {
		return c.err
	}
	c.committed = append(c.committed, drained...)
	return nil
}

// recordingDeadLetters records dead-letter quarantine writes and replay
// completions so the harness can assert the dead-letter and replay-clear paths.
type recordingDeadLetters struct {
	records    []collector.GenerationDeadLetter
	replayDone []collector.GenerationDeadLetterReplayCompletion
}

func (d *recordingDeadLetters) RecordGenerationDeadLetter(
	_ context.Context,
	record collector.GenerationDeadLetter,
) error {
	d.records = append(d.records, record)
	return nil
}

func (d *recordingDeadLetters) CompleteGenerationDeadLetterReplay(
	_ context.Context,
	completion collector.GenerationDeadLetterReplayCompletion,
) error {
	d.replayDone = append(d.replayDone, completion)
	return nil
}
