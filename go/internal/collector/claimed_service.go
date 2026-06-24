package collector

import (
	"context"
	"errors"
	"fmt"
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

var errRetryableClaimRecorded = errors.New("retryable claim failure recorded")
var errTerminalClaimRecorded = errors.New("terminal claim failure recorded")

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
	MaxAttempts         int
	Clock               func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
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

func (s ClaimedService) claimNext(
	ctx context.Context,
	claimID string,
) (workflow.WorkItem, workflow.Claim, workflow.ClaimTarget, bool, error) {
	if s.ClaimDispatcher != nil {
		return s.ClaimDispatcher.ClaimNext(ctx, s.OwnerID, claimID, s.now(), s.ClaimLeaseTTL)
	}
	target := workflow.ClaimTarget{
		CollectorKind:       s.CollectorKind,
		CollectorInstanceID: s.CollectorInstanceID,
	}
	item, claim, found, err := s.ControlStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       target.CollectorKind,
		CollectorInstanceID: target.CollectorInstanceID,
		OwnerID:             s.OwnerID,
		ClaimID:             claimID,
	}, s.now(), s.ClaimLeaseTTL)
	return item, claim, target, found, err
}

func (s ClaimedService) processClaimed(ctx context.Context, item workflow.WorkItem, claim workflow.Claim) error {
	if s.Tracer != nil && s.CollectorKind == scope.CollectorTerraformState {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateClaimProcess)
		defer span.End()
	}
	s.recordWorkflowClaimWait(ctx, item)

	// Per-collector run duration: record on every return path so the operator
	// can see the long pole per collector_kind without joining claim-state tables.
	// startedAt and runOutcome are call-local; concurrent workers never share them.
	runStartedAt := s.now()
	runOutcome := telemetry.CollectorRunOutcomeFailTerminal // pessimistic default; overwritten below
	defer func() {
		s.recordClaimRunDuration(ctx, item, runStartedAt, runOutcome)
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
			return s.failTerminal(ctx, mutation, "collect_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
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
			if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", err)); failErr != nil {
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
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", err)); failErr != nil {
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
			return s.failTerminal(ctx, mutation, "commit_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			runOutcome = telemetry.CollectorRunOutcomeFailTerminal
			return s.failTerminal(ctx, mutation, FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
		}
		runOutcome = telemetry.CollectorRunOutcomeFailRetryable
		return s.failRetryable(ctx, mutation, item, "commit_failure", err)
	}
	// Successful commit: record per-collector fact volume before completing.
	s.recordClaimFactsEmitted(ctx, item, collected)
	completeMutation, err := s.resolvedCompletionMutation(mutation, collected)
	if err != nil {
		runOutcome = telemetry.CollectorRunOutcomeFailTerminal
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", err)); failErr != nil {
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
	failureClass string,
	err error,
) error {
	failureClass = classifiedFailureClass(err, failureClass)
	failed := withFailure(mutation, failureClass, err)
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

func (s ClaimedService) failTerminal(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	failureClass string,
	err error,
) error {
	failureClass = classifiedFailureClass(err, failureClass)
	failed := withFailure(mutation, failureClass, err)
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
