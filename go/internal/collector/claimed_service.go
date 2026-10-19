// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimControlStore is the workflow claim surface needed by a claim-aware
// collector runner.
type ClaimControlStore interface {
	ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error)
	HeartbeatClaim(context.Context, workflow.ClaimMutation) error
	CompleteClaim(context.Context, workflow.ClaimMutation) error
	ReleaseClaim(context.Context, workflow.ClaimMutation) error
	FailClaimRetryable(context.Context, workflow.ClaimMutation) error
	FailClaimTerminal(context.Context, workflow.ClaimMutation) error
}

var (
	errRetryableClaimRecorded = errors.New("retryable claim failure recorded")
	errTerminalClaimRecorded  = errors.New("terminal claim failure recorded")
)

// ClaimedSource resolves one already-claimed work item into a collected
// generation that can be committed through the normal collector path.
type ClaimedSource interface {
	NextClaimed(context.Context, workflow.WorkItem) (CollectedGeneration, bool, error)
}

// FailureClassAttemptBudgetExhausted labels claims that the bounded retry
// guard routed to FailClaimTerminal after the work item's prior AttemptCount
// reached the configured MaxAttempts. The guard exists so a recurring
// retryable failure (stale fence, unauthorized API, blocked network) cannot
// drive workflow_claims.failed_retryable into the millions — the runtime
// shape seen in issue #612 before this guard landed.
const FailureClassAttemptBudgetExhausted = "attempt_budget_exhausted"

// ClaimedService runs a collector through durable workflow claims. It is an
// opt-in runner and does not replace the existing unclaimed ingester path.
//
// MaxAttempts is the bounded retry budget for one work item. When greater
// than zero, a retryable failure on a claim whose work item AttemptCount has
// already reached MaxAttempts is escalated to FailClaimTerminal with class
// FailureClassAttemptBudgetExhausted. MaxAttempts == 0 preserves the legacy
// unbounded behavior for callers that have not yet wired a budget.
type ClaimedService struct {
	ControlStore    ClaimControlStore
	ClaimDispatcher ClaimDispatcher
	Source          ClaimedSource
	// SourceResolver resolves the claim-aware source adapter for one dispatched
	// claim target (collector kind and instance id). When a ClaimDispatcher
	// selects targets across multiple collector families and instances, the
	// runner resolves the matching source per target. When nil, the single
	// Source is used for every claimed target (the single-family runner).
	// Resolution must consider the instance id, not just the kind, because some
	// claim-aware sources reject work whose CollectorInstanceID does not match
	// their configured instance.
	SourceResolver      func(target workflow.ClaimTarget) (ClaimedSource, bool)
	Committer           Committer
	DeadLetters         GenerationDeadLetterSink
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	OwnerID             string
	ClaimIDFunc         func() string
	PollInterval        time.Duration
	ClaimLeaseTTL       time.Duration
	HeartbeatInterval   time.Duration
	// ClaimPacing, when set to an enabled config, paces grants on
	// per-domain depth first with global depth as the ceiling (#8470). The
	// control store must serve CheckClaimPacing (WorkflowControlStore
	// does); any other store claims unpaced. Nil (the default) claims with
	// no pacing consult.
	ClaimPacing *workflow.ClaimPacingConfig
	MaxAttempts int
	Clock       func() time.Time
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	// Logger records fail-open pacing consults on the undispatched grant
	// path. Optional — nil means no structured logging.
	Logger *slog.Logger
}

// Run claims bounded work, emits facts through the existing commit boundary,
// and completes or releases the durable claim with fencing.
func (s ClaimedService) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		claimID := strings.TrimSpace(s.ClaimIDFunc())
		if claimID == "" {
			return fmt.Errorf("claim id is required")
		}
		item, claim, target, found, err := s.claimNext(ctx, claimID)
		if err != nil {
			var deferral *workflow.ClaimPacingDeferral
			if errors.As(err, &deferral) {
				// A paced grant is flow control, not failure: no claim
				// was issued, so back off for the longer of the poll
				// interval and the grant delay on the existing
				// retry-after path, then claim again.
				if waitErr := waitForNextPoll(ctx, max(s.PollInterval, deferral.RetryAfterDelay())); waitErr != nil {
					return nil
				}
				continue
			}
			return fmt.Errorf("claim next %s work item: %w", s.claimedKindLabel(), err)
		}
		if !found {
			if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}

		runner := s
		runner.CollectorKind = target.CollectorKind
		runner.CollectorInstanceID = target.CollectorInstanceID
		runner.Source, err = s.resolveClaimedSource(target)
		if err != nil {
			// The dispatcher selected a family with no registered source. Release
			// the held claim so the work is not stranded, then surface the
			// misconfiguration rather than processing with the wrong source.
			if releaseErr := s.ControlStore.ReleaseClaim(ctx, runner.claimMutation(item, claim)); releaseErr != nil {
				return fmt.Errorf("release unresolved %s claim: %w", target.CollectorKind, releaseErr)
			}
			return err
		}
		if err := runner.processClaimed(ctx, item, claim); err != nil {
			if errors.Is(err, errRetryableClaimRecorded) || errors.Is(err, errTerminalClaimRecorded) {
				continue
			}
			return err
		}
	}
}

func (s ClaimedService) validate() error {
	if s.ControlStore == nil {
		return errors.New("claim control store is required")
	}
	if s.Source == nil && s.SourceResolver == nil {
		return errors.New("a claimed source or source resolver is required")
	}
	if s.Committer == nil {
		return errors.New("collector committer is required")
	}
	if _, ok := s.Committer.(ClaimedCommitter); !ok {
		return errors.New("claim-aware collector committer must implement ClaimedCommitter")
	}
	if s.ClaimDispatcher == nil {
		if strings.TrimSpace(string(s.CollectorKind)) == "" {
			return errors.New("collector kind is required")
		}
		if strings.TrimSpace(s.CollectorInstanceID) == "" {
			return errors.New("collector instance id is required")
		}
	}
	if strings.TrimSpace(s.OwnerID) == "" {
		return errors.New("owner id is required")
	}
	if s.ClaimIDFunc == nil {
		return errors.New("claim id function is required")
	}
	if s.PollInterval <= 0 {
		return errors.New("collector poll interval must be positive")
	}
	if s.ClaimLeaseTTL <= 0 {
		return errors.New("claim lease TTL must be positive")
	}
	if s.HeartbeatInterval <= 0 {
		return errors.New("claim heartbeat interval must be positive")
	}
	if s.HeartbeatInterval >= s.ClaimLeaseTTL {
		return errors.New("claim heartbeat interval must be less than lease TTL")
	}
	return nil
}

func (s ClaimedService) processClaimed(ctx context.Context, item workflow.WorkItem, claim workflow.Claim) error {
	// Per-collector run duration: record on every return path so the operator
	// can see the long pole per collector_kind without joining claim-state tables.
	// startedAt and runOutcome are call-local; concurrent workers never share them.
	runStartedAt := s.now()
	runOutcome := telemetry.CollectorRunOutcomeFailTerminal // pessimistic default; overwritten below

	// collector.claimed_run wraps the whole claim-to-ack cycle for every
	// collector family so a trace correlates with the per-collector
	// eshu_dp_workflow_claim_run_duration_seconds metric. The span attributes
	// mirror the metric labels (collector_kind, source_system, outcome). The
	// outcome is set in the End closure so it reflects the final return path.
	var runSpan trace.Span
	if s.Tracer != nil {
		ctx, runSpan = s.Tracer.Start(ctx, telemetry.SpanCollectorClaimedRun)
		runSpan.SetAttributes(
			telemetry.AttrCollectorKind(string(s.CollectorKind)),
			telemetry.AttrSourceSystem(item.SourceSystem),
			telemetry.AttrFairnessKey(item.FairnessKey),
			telemetry.AttrClaimPriority(item.ClaimPriority),
		)
	}

	if s.Tracer != nil && s.CollectorKind == scope.CollectorTerraformState {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateClaimProcess)
		defer span.End()
	}
	s.recordWorkflowClaimWait(ctx, item)

	defer func() {
		s.recordClaimRunDuration(ctx, item, runStartedAt, runOutcome)
		if runSpan != nil {
			runSpan.SetAttributes(telemetry.AttrOutcome(runOutcome))
			runSpan.End()
		}
	}()

	mutation := s.claimMutation(item, claim)
	if err := s.ControlStore.HeartbeatClaim(ctx, mutation); err != nil {
		return fmt.Errorf("heartbeat claimed %s work item: %w", s.claimedKindLabel(), err)
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatErr := s.startHeartbeatLoop(heartbeatCtx, mutation, item)
	defer stopHeartbeat()

	collected, ok, err := s.Source.NextClaimed(ctx, item)
	if err != nil {
		if isTerminalFailure(err) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, "collect_failure", "collect_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, "collect_failure", FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
		}
		runOutcome = telemetry.CollectorRunOutcomeFailRetryable
		return s.failRetryable(ctx, mutation, item, "collect_failure", err)
	}
	if !ok {
		stopHeartbeat()
		if err := s.ControlStore.ReleaseClaim(ctx, mutation); err != nil {
			return fmt.Errorf("release claimed %s work item: %w", s.claimedKindLabel(), err)
		}
		runOutcome = telemetry.CollectorRunOutcomeReleased
		return nil
	}
	if collected.Unchanged {
		completeMutation, err := s.resolvedCompletionMutation(mutation, collected)
		if err != nil {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", "identity_mismatch", true, err)); failErr != nil {
				return fmt.Errorf("terminal-fail mismatched %s claim: %w", s.claimedKindLabel(), failErr)
			}
			return err
		}
		stopHeartbeat()
		if err := drainHeartbeatError(heartbeatErr); err != nil {
			return err
		}
		if err := s.ControlStore.CompleteClaim(ctx, completeMutation); err != nil {
			return fmt.Errorf("complete unchanged claimed %s work item: %w", s.claimedKindLabel(), err)
		}
		runOutcome = telemetry.CollectorRunOutcomeUnchanged
		return s.completeGenerationDeadLetterReplay(ctx, collected)
	}
	if err := validateClaimedGeneration(item, collected); err != nil {
		stopHeartbeat()
		if cleanupErr := cleanupCollectedFactStream(collected); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		runOutcome = telemetry.CollectorRunOutcomeFailTerminal
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", "identity_mismatch", true, err)); failErr != nil {
			return fmt.Errorf("terminal-fail mismatched %s claim: %w", s.claimedKindLabel(), failErr)
		}
		return err
	}
	commitMutation := mutation
	commitMutation.ObservedAt = s.now()
	if err := s.commitCollected(ctx, commitMutation, collected); err != nil {
		err = s.recordGenerationDeadLetter(ctx, collected, "commit_failure", err)
		// Mirror the NextClaimed path: a commit-side terminal classification
		// (for example awsruntime stale-fence on CommitAWSScan) must route to
		// FailClaimTerminal so the same orphaned-row loop issue #612 was
		// opened to break cannot resurface through the commit path.
		if isTerminalFailure(err) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, "commit_failure", "commit_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, "commit_failure", FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
		}
		runOutcome = telemetry.CollectorRunOutcomeFailRetryable
		return s.failRetryable(ctx, mutation, item, "commit_failure", err)
	}
	// #5429: give any source-owned post-commit progress marker (for example
	// ghactionsruntime's cross-cycle run watermark) a chance to advance now
	// that the commit is durable -- never before, and never on a failed
	// commit above. See ClaimedGenerationCommitObserver's doc comment.
	s.observeClaimedGenerationCommitted(ctx, item)
	// Successful commit: record per-collector fact volume before completing.
	s.recordClaimFactsEmitted(ctx, item, collected)
	completeMutation, err := s.resolvedCompletionMutation(mutation, collected)
	if err != nil {
		runOutcome = telemetry.CollectorRunOutcomeFailTerminal
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", "identity_mismatch", true, err)); failErr != nil {
			return fmt.Errorf("terminal-fail mismatched %s claim: %w", s.claimedKindLabel(), failErr)
		}
		return err
	}
	stopHeartbeat()
	if err := drainHeartbeatError(heartbeatErr); err != nil {
		return err
	}
	if err := s.ControlStore.CompleteClaim(ctx, completeMutation); err != nil {
		return fmt.Errorf("complete claimed %s work item: %w", s.claimedKindLabel(), err)
	}
	runOutcome = telemetry.CollectorRunOutcomeSuccess
	return s.completeGenerationDeadLetterReplay(ctx, collected)
}

func (s ClaimedService) commitCollected(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	collected CollectedGeneration,
) error {
	if s.Tracer != nil && s.CollectorKind == scope.CollectorTerraformState {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateFactEmitBatch)
		defer span.End()
	}
	if committer, ok := s.Committer.(ClaimedCommitter); ok {
		if collected.FactStreamErr != nil {
			streamCommitter, ok := s.Committer.(StreamErrorClaimedCommitter)
			if !ok {
				if err := cleanupCollectedFactStream(collected); err != nil {
					return err
				}
				return errors.New("claim-aware collector committer must support fact stream errors")
			}
			return streamCommitter.CommitClaimedScopeGenerationWithStreamError(
				ctx,
				mutation,
				collected.Scope,
				collected.Generation,
				collected.Facts,
				collected.FactStreamErr,
			)
		}
		return committer.CommitClaimedScopeGeneration(
			ctx,
			mutation,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		)
	}
	return errors.New("claim-aware collector committer must implement ClaimedCommitter")
}

func cleanupCollectedFactStream(collected CollectedGeneration) error {
	drainFactStream(collected.Facts)
	if collected.FactStreamErr == nil {
		return nil
	}
	if err := collected.FactStreamErr(); err != nil {
		return fmt.Errorf("read fact stream: %w", err)
	}
	return nil
}

func (s ClaimedService) startHeartbeatLoop(ctx context.Context, mutation workflow.ClaimMutation, item workflow.WorkItem) <-chan error {
	errc := make(chan error, 1)
	claimStart := s.now()
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next := mutation
				next.ObservedAt = s.now()
				if err := s.ControlStore.HeartbeatClaim(ctx, next); err != nil {
					errc <- fmt.Errorf("heartbeat claimed %s work item: %w", s.claimedKindLabel(), err)
					return
				}
				s.recordClaimLeaseAge(ctx, item, next.ObservedAt.Sub(claimStart).Seconds())
			}
		}
	}()
	return errc
}

// attemptBudgetExhausted reports whether the current claim's work item has
// already consumed its configured retry budget. MaxAttempts == 0 disables the
// guard so legacy callers that have not yet wired a bounded retry policy
// keep their unbounded behavior; the AWS, terraform-state, and scanner
// runners that suffered the runaway loop in issue #612 explicitly set a
// positive budget.
func (s ClaimedService) attemptBudgetExhausted(item workflow.WorkItem) bool {
	if s.MaxAttempts <= 0 {
		return false
	}
	return item.AttemptCount >= s.MaxAttempts
}

func (s ClaimedService) budgetExhaustedError(ctx context.Context, item workflow.WorkItem, cause error) error {
	s.recordAttemptBudgetExhausted(ctx, item)
	return fmt.Errorf(
		"%s work item attempt %d exhausted retry budget %d: %w",
		s.claimedKindLabel(),
		item.AttemptCount,
		s.MaxAttempts,
		cause,
	)
}

func (s ClaimedService) failRetryable(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	item workflow.WorkItem,
	operation string,
	err error,
) error {
	failureClass := classifiedFailureClass(err, operation)
	failed := withFailure(mutation, operation, failureClass, false, err)
	if failed.VisibleAt.IsZero() {
		failed.VisibleAt = s.retryableVisibleAt(err)
	}
	if failErr := s.ControlStore.FailClaimRetryable(ctx, failed); failErr != nil {
		return fmt.Errorf("retryable-fail claimed %s work item: %w", s.claimedKindLabel(), failErr)
	}
	s.recordClaimRetry(ctx, item, failureClass)
	s.recordProviderThrottle(ctx, item, failureClass, err)
	return errors.Join(errRetryableClaimRecorded, fmt.Errorf("%s: %w", failureClass, err))
}

// failTerminal persists a terminal claim failure. operation is the
// claim-stage token recorded in the operation column (for example
// "collect_failure"); classFallback seeds the class column only when the
// error chain carries no class of its own. The two stay separate so a class
// constant never leaks into the operation taxonomy.
func (s ClaimedService) failTerminal(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	operation string,
	classFallback string,
	err error,
) error {
	failureClass := classifiedFailureClass(err, classFallback)
	failed := withFailure(mutation, operation, failureClass, true, err)
	if failErr := s.ControlStore.FailClaimTerminal(ctx, failed); failErr != nil {
		return fmt.Errorf("terminal-fail claimed %s work item: %w", s.claimedKindLabel(), failErr)
	}
	return errors.Join(errTerminalClaimRecorded, fmt.Errorf("%s: %w", failureClass, err))
}

func (s ClaimedService) claimedKindLabel() string {
	return string(s.CollectorKind)
}

func (s ClaimedService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
