// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// FailureClassSourceStateRecord marks a completed claim whose attempt row
// could not be recorded through the ClaimOutcomeRecorder. The claim fails
// retryable, matching the in-tree vulnerability collector, which fails the
// claim when its attempt checkpoint cannot be written.
const FailureClassSourceStateRecord = "source_state_record"

// maxPendingClaimOutcomes bounds how many completed outcomes may wait for
// their commit hook. An entry is consumed by ObserveClaimedGenerationCommitted
// and cleared on every NextClaimed exit for its work item, so it only stays
// when a commit never succeeds (a terminal commit failure, a lost process). A
// worker holds one claim at a time, so the cap is far above the steady state;
// on overflow the entry with the oldest attempt time is evicted, which leaves
// that target's row pending, never wrong.
const maxPendingClaimOutcomes = 256

// ClaimOutcomeRecorder is the optional host seam that turns claim outcomes
// into durable source-owned state (#9420: vulnerability_source_states, whose
// writer moved from the retired in-tree collector to the host). Extensions
// never see it: the host calls it, so no storage handle crosses the stdio
// boundary.
type ClaimOutcomeRecorder interface {
	// RecordClaimOutcome is called exactly once per NextClaimed call, on every
	// exit path, after the outcome is decided. A completed outcome records an
	// attempt (pending) row; a failed outcome records the failure row. An
	// error on a completed outcome fails the claim retryable with
	// FailureClassSourceStateRecord; on a failed outcome it is joined to the
	// claim error and never changes its classification.
	RecordClaimOutcome(context.Context, ClaimOutcome) error
	// RecordClaimCommitted is called at most once for a completed outcome,
	// from ObserveClaimedGenerationCommitted after ClaimedService durably
	// commits the generation (or resolves an unchanged one). An error is
	// non-fatal: the facts are durable and the row stays pending.
	RecordClaimCommitted(context.Context, ClaimOutcome) error
}

// ClaimOutcome is the decided outcome of one claim. Unlike ResultRecord it
// carries the work item, because a recorder keys durable state by scope; it
// must not be used for metric labels.
type ClaimOutcome struct {
	// Item is the claimed work item NextClaimed received.
	Item   workflow.WorkItem
	Origin ResultOrigin
	// State is the SDK result state, or for a host verdict the state
	// ClaimedService applies (terminal or retryable).
	State        sdkcollector.ResultState
	FailureClass string
	// Terminal reports whether ClaimedService fails the claim terminal.
	Terminal bool
	// RetryAfter is the extension's capped retry request (zero unless
	// retryable); ClaimedService applies max(PollInterval, RetryAfter).
	RetryAfter time.Duration
	// Partial is true when the result state is partial or any status row
	// reported partial data.
	Partial      bool
	FactCount    int
	WarningCount int
	// AttemptedAt is the host clock when NextClaimed started; both rows a
	// claim writes carry it so a store can order outcomes by attempt.
	AttemptedAt time.Time
	// ObservedAt is the result generation's observed time for SDK results.
	ObservedAt time.Time
}

// Completed reports whether the claim produced a generation ClaimedService
// commits (complete or partial) or resolves (unchanged).
func (o ClaimOutcome) Completed() bool {
	switch o.State {
	case sdkcollector.ResultComplete, sdkcollector.ResultUnchanged, sdkcollector.ResultPartial:
		return true
	default:
		return false
	}
}

// claimOutcome builds the outcome for one decided claim. err is always an
// extensionFailure or nil here.
func claimOutcome(
	item workflow.WorkItem,
	attemptedAt time.Time,
	result sdkcollector.Result,
	origin ResultOrigin,
	err error,
) ClaimOutcome {
	outcome := ClaimOutcome{Item: item, Origin: origin, AttemptedAt: attemptedAt}
	failure, failed := err.(extensionFailure)
	if failed {
		outcome.FailureClass = failure.class
		outcome.Terminal = failure.terminal
		outcome.RetryAfter = failure.retryAfter
	}
	if origin == ResultOriginHost {
		outcome.State = sdkcollector.ResultRetryable
		if failure.terminal {
			outcome.State = sdkcollector.ResultTerminal
		}
		return outcome
	}
	outcome.State = result.State
	outcome.ObservedAt = result.Generation.ObservedAt.UTC()
	outcome.Partial = result.State == sdkcollector.ResultPartial
	for _, status := range result.Statuses {
		outcome.FactCount += status.FactCount
		outcome.WarningCount += status.WarningCount
		outcome.Partial = outcome.Partial || status.Partial
	}
	return outcome
}

// recordClaimOutcome reports the decided outcome to the recorder and applies
// the failure semantics documented on ClaimOutcomeRecorder. It returns the
// possibly rewritten NextClaimed results.
func (s *Source) recordClaimOutcome(
	ctx context.Context,
	item workflow.WorkItem,
	attemptedAt time.Time,
	collected collector.CollectedGeneration,
	ok bool,
	result sdkcollector.Result,
	origin ResultOrigin,
	err error,
) (collector.CollectedGeneration, bool, ResultOrigin, error) {
	if s.outcomeRecorder == nil {
		return collected, ok, origin, err
	}
	s.pendingOutcomes.drop(item)
	outcome := claimOutcome(item, attemptedAt, result, origin, err)
	recordErr := s.outcomeRecorder.RecordClaimOutcome(ctx, outcome)
	if err == nil && outcome.Completed() {
		if recordErr != nil {
			// The generation is a buffered, closed channel; dropping it
			// leaks nothing. ClaimedService retries the claim.
			return collector.CollectedGeneration{}, false, ResultOriginHost, extensionFailure{
				class: FailureClassSourceStateRecord,
				cause: recordErr,
			}
		}
		s.pendingOutcomes.stash(outcome)
		return collected, ok, origin, err
	}
	if recordErr == nil {
		return collected, ok, origin, err
	}
	var failure extensionFailure
	if errors.As(err, &failure) {
		failure.cause = errors.Join(failure.cause, fmt.Errorf("record claim outcome: %w", recordErr))
		return collected, ok, origin, failure
	}
	return collected, ok, origin, errors.Join(err, fmt.Errorf("record claim outcome: %w", recordErr))
}

// ObserveClaimedGenerationCommitted implements
// collector.ClaimedGenerationCommitObserver. ClaimedService calls it after
// the claim's generation is durably committed (or an unchanged result is
// resolved); the Source then hands the stashed completed outcome to the
// recorder. Absent a stashed outcome (no recorder, a failed claim, or a
// second call) it does nothing. Errors are non-fatal by the observer
// contract.
func (s *Source) ObserveClaimedGenerationCommitted(ctx context.Context, item workflow.WorkItem) error {
	if s.outcomeRecorder == nil {
		return nil
	}
	outcome, ok := s.pendingOutcomes.take(item)
	if !ok {
		return nil
	}
	return s.outcomeRecorder.RecordClaimCommitted(ctx, outcome)
}

// pendingClaimOutcomes stashes completed outcomes between NextClaimed and the
// commit hook, keyed by work item id. It follows ghactionsruntime's
// pendingWatermarks: one mutex-guarded map behind a pointer shared by every
// copy of the Source, because several ClaimedService workers can run against
// one Source. Workers never contend on one key, since a work item row has at
// most one live claim (FOR UPDATE SKIP LOCKED); they share only the map.
type pendingClaimOutcomes struct {
	mu   sync.Mutex
	rows map[string]ClaimOutcome
}

func newPendingClaimOutcomes() *pendingClaimOutcomes {
	return &pendingClaimOutcomes{rows: make(map[string]ClaimOutcome)}
}

// stash records a completed outcome, overwriting an earlier attempt of the
// same work item and evicting the oldest entry at the cap.
func (p *pendingClaimOutcomes) stash(outcome ClaimOutcome) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := outcome.Item.WorkItemID
	if _, exists := p.rows[key]; !exists && len(p.rows) >= maxPendingClaimOutcomes {
		oldestKey := ""
		var oldest time.Time
		for candidate, row := range p.rows {
			if oldestKey == "" || row.AttemptedAt.Before(oldest) {
				oldestKey, oldest = candidate, row.AttemptedAt
			}
		}
		delete(p.rows, oldestKey)
	}
	p.rows[key] = outcome
}

// take returns and removes the stashed outcome for item. An entry stashed
// for a different generation of the same work item is left in place.
func (p *pendingClaimOutcomes) take(item workflow.WorkItem) (ClaimOutcome, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	outcome, ok := p.rows[item.WorkItemID]
	if !ok || outcome.Item.GenerationID != item.GenerationID {
		return ClaimOutcome{}, false
	}
	delete(p.rows, item.WorkItemID)
	return outcome, true
}

// drop clears any stashed outcome for item; NextClaimed calls it on every
// exit before recording the new outcome.
func (p *pendingClaimOutcomes) drop(item workflow.WorkItem) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.rows, item.WorkItemID)
}

func (p *pendingClaimOutcomes) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.rows)
}
