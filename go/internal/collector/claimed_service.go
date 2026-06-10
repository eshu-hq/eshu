package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"
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
	ControlStore        ClaimControlStore
	Source              ClaimedSource
	Committer           Committer
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
		item, claim, found, err := s.ControlStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       s.CollectorKind,
			CollectorInstanceID: s.CollectorInstanceID,
			OwnerID:             s.OwnerID,
			ClaimID:             claimID,
		}, s.now(), s.ClaimLeaseTTL)
		if err != nil {
			return fmt.Errorf("claim next %s work item: %w", s.claimedKindLabel(), err)
		}
		if !found {
			if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}

		if err := s.processClaimed(ctx, item, claim); err != nil {
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
	if s.Source == nil {
		return errors.New("claimed source is required")
	}
	if s.Committer == nil {
		return errors.New("collector committer is required")
	}
	if _, ok := s.Committer.(ClaimedCommitter); !ok {
		return errors.New("claim-aware collector committer must implement ClaimedCommitter")
	}
	if strings.TrimSpace(string(s.CollectorKind)) == "" {
		return errors.New("collector kind is required")
	}
	if strings.TrimSpace(s.CollectorInstanceID) == "" {
		return errors.New("collector instance id is required")
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
	if s.Tracer != nil && s.CollectorKind == scope.CollectorTerraformState {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateClaimProcess)
		defer span.End()
	}
	s.recordTerraformStateClaimWait(ctx, item)

	mutation := s.claimMutation(item, claim)
	if err := s.ControlStore.HeartbeatClaim(ctx, mutation); err != nil {
		return fmt.Errorf("heartbeat claimed %s work item: %w", s.claimedKindLabel(), err)
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatErr := s.startHeartbeatLoop(heartbeatCtx, mutation)
	defer stopHeartbeat()

	collected, ok, err := s.Source.NextClaimed(ctx, item)
	if err != nil {
		if isTerminalFailure(err) {
			return s.failTerminal(ctx, mutation, "collect_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			return s.failTerminal(ctx, mutation, FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
		}
		return s.failRetryable(ctx, mutation, "collect_failure", err)
	}
	if !ok {
		stopHeartbeat()
		if err := s.ControlStore.ReleaseClaim(ctx, mutation); err != nil {
			return fmt.Errorf("release claimed %s work item: %w", s.claimedKindLabel(), err)
		}
		return nil
	}
	if collected.Unchanged {
		completeMutation, err := s.resolvedCompletionMutation(mutation, collected)
		if err != nil {
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
		return nil
	}
	if err := validateClaimedGeneration(item, collected); err != nil {
		stopHeartbeat()
		if cleanupErr := cleanupCollectedFactStream(collected); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", err)); failErr != nil {
			return fmt.Errorf("terminal-fail mismatched %s claim: %w", s.claimedKindLabel(), failErr)
		}
		return err
	}
	commitMutation := mutation
	commitMutation.ObservedAt = s.now()
	if err := s.commitCollected(ctx, commitMutation, collected); err != nil {
		// Mirror the NextClaimed path: a commit-side terminal classification
		// (for example awsruntime stale-fence on CommitAWSScan) must route to
		// FailClaimTerminal so the same orphaned-row loop issue #612 was
		// opened to break cannot resurface through the commit path.
		if isTerminalFailure(err) {
			return s.failTerminal(ctx, mutation, "commit_failure", err)
		}
		if s.attemptBudgetExhausted(item) {
			return s.failTerminal(ctx, mutation, FailureClassAttemptBudgetExhausted, s.budgetExhaustedError(ctx, item, err))
		}
		return s.failRetryable(ctx, mutation, "commit_failure", err)
	}
	completeMutation, err := s.resolvedCompletionMutation(mutation, collected)
	if err != nil {
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
	return nil
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

func (s ClaimedService) startHeartbeatLoop(ctx context.Context, mutation workflow.ClaimMutation) <-chan error {
	errc := make(chan error, 1)
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

func (s ClaimedService) recordAttemptBudgetExhausted(ctx context.Context, item workflow.WorkItem) {
	if s.Instruments == nil || s.Instruments.WorkflowClaimAttemptBudgetExhausted == nil {
		return
	}
	s.Instruments.WorkflowClaimAttemptBudgetExhausted.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(string(s.CollectorKind)),
		telemetry.AttrSourceSystem(item.SourceSystem),
	))
}

func (s ClaimedService) failRetryable(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	failureClass string,
	err error,
) error {
	failureClass = classifiedFailureClass(err, failureClass)
	failed := withFailure(mutation, failureClass, err)
	if failed.VisibleAt.IsZero() {
		failed.VisibleAt = s.now().Add(s.PollInterval)
	}
	if failErr := s.ControlStore.FailClaimRetryable(ctx, failed); failErr != nil {
		return fmt.Errorf("retryable-fail claimed %s work item: %w", s.claimedKindLabel(), failErr)
	}
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

func (s ClaimedService) recordTerraformStateClaimWait(ctx context.Context, item workflow.WorkItem) {
	if s.CollectorKind != scope.CollectorTerraformState || s.Instruments == nil {
		return
	}
	reference := item.VisibleAt
	if reference.IsZero() {
		reference = item.CreatedAt
	}
	if reference.IsZero() {
		return
	}
	wait := s.now().Sub(reference.UTC()).Seconds()
	if wait < 0 {
		wait = 0
	}
	s.Instruments.TerraformStateClaimWaitDuration.Record(ctx, wait, metric.WithAttributes(
		telemetry.AttrSourceSystem(item.SourceSystem),
		telemetry.AttrCollectorKind(string(item.CollectorKind)),
	))
}

func validateClaimedGeneration(item workflow.WorkItem, collected CollectedGeneration) error {
	if collected.Scope.ScopeID != item.ScopeID {
		return fmt.Errorf("claimed scope_id %q produced scope_id %q", item.ScopeID, collected.Scope.ScopeID)
	}
	if collected.Scope.SourceSystem != item.SourceSystem {
		return fmt.Errorf("claimed source_system %q produced source_system %q", item.SourceSystem, collected.Scope.SourceSystem)
	}
	if collected.Scope.CollectorKind != item.CollectorKind {
		return fmt.Errorf("claimed collector_kind %q produced collector_kind %q", item.CollectorKind, collected.Scope.CollectorKind)
	}
	if item.CollectorKind == scope.CollectorTerraformState {
		if err := collected.Generation.ValidateForScope(collected.Scope); err != nil {
			return fmt.Errorf("validate claimed terraform state generation: %w", err)
		}
		if strings.TrimSpace(collected.Generation.FreshnessHint) == "" {
			return fmt.Errorf("claimed terraform state generation freshness hint must not be blank")
		}
		return nil
	}
	if collected.Generation.GenerationID != item.GenerationID {
		return fmt.Errorf("claimed generation_id %q produced generation_id %q", item.GenerationID, collected.Generation.GenerationID)
	}
	if collected.Generation.GenerationID != item.SourceRunID {
		return fmt.Errorf("claimed source_run_id %q produced generation_id %q", item.SourceRunID, collected.Generation.GenerationID)
	}
	return nil
}

func withFailure(mutation workflow.ClaimMutation, failureClass string, err error) workflow.ClaimMutation {
	mutation.FailureClass = failureClass
	if err != nil {
		mutation.FailureMessage = err.Error()
	}
	return mutation
}

type classifiedFailure interface {
	FailureClass() string
}

type terminalFailure interface {
	TerminalFailure() bool
}

func classifiedFailureClass(err error, fallback string) string {
	var classified classifiedFailure
	if errors.As(err, &classified) {
		if value := strings.TrimSpace(classified.FailureClass()); value != "" {
			return value
		}
	}
	return fallback
}

func isTerminalFailure(err error) bool {
	var terminal terminalFailure
	return errors.As(err, &terminal) && terminal.TerminalFailure()
}

func drainHeartbeatError(errc <-chan error) error {
	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}
