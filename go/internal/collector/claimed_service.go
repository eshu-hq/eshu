package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
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

// ClaimedSource resolves one already-claimed work item into a collected
// generation that can be committed through the normal collector path.
type ClaimedSource interface {
	NextClaimed(context.Context, workflow.WorkItem) (CollectedGeneration, bool, error)
}

// ClaimedService runs a collector through durable workflow claims. It is an
// opt-in runner and does not replace the existing unclaimed ingester path.
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
	Clock               func() time.Time
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
			return fmt.Errorf("claim next git work item: %w", err)
		}
		if !found {
			if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}

		if err := s.processClaimed(ctx, item, claim); err != nil {
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
	mutation := s.claimMutation(item, claim)
	if err := s.ControlStore.HeartbeatClaim(ctx, mutation); err != nil {
		return fmt.Errorf("heartbeat claimed git work item: %w", err)
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatErr := s.startHeartbeatLoop(heartbeatCtx, mutation)
	defer stopHeartbeat()

	collected, ok, err := s.Source.NextClaimed(ctx, item)
	if err != nil {
		return s.failRetryable(ctx, mutation, "collect_failure", err)
	}
	if !ok {
		stopHeartbeat()
		if err := s.ControlStore.ReleaseClaim(ctx, mutation); err != nil {
			return fmt.Errorf("release claimed git work item: %w", err)
		}
		return nil
	}
	if err := validateClaimedGeneration(item, collected); err != nil {
		stopHeartbeat()
		if failErr := s.ControlStore.FailClaimTerminal(ctx, withFailure(mutation, "identity_mismatch", err)); failErr != nil {
			return fmt.Errorf("terminal-fail mismatched git claim: %w", failErr)
		}
		return err
	}
	if err := s.Committer.CommitScopeGeneration(ctx, collected.Scope, collected.Generation, collected.Facts); err != nil {
		return s.failRetryable(ctx, mutation, "commit_failure", err)
	}
	stopHeartbeat()
	if err := drainHeartbeatError(heartbeatErr); err != nil {
		return err
	}
	if err := s.ControlStore.CompleteClaim(ctx, mutation); err != nil {
		return fmt.Errorf("complete claimed git work item: %w", err)
	}
	return nil
}

func (s ClaimedService) claimMutation(item workflow.WorkItem, claim workflow.Claim) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claim.ClaimID,
		FencingToken:  claim.FencingToken,
		OwnerID:       claim.OwnerID,
		ObservedAt:    s.now(),
		LeaseDuration: s.ClaimLeaseTTL,
	}
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
					errc <- fmt.Errorf("heartbeat claimed git work item: %w", err)
					return
				}
			}
		}
	}()
	return errc
}

func (s ClaimedService) failRetryable(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	failureClass string,
	err error,
) error {
	if failErr := s.ControlStore.FailClaimRetryable(ctx, withFailure(mutation, failureClass, err)); failErr != nil {
		return fmt.Errorf("retryable-fail claimed git work item: %w", failErr)
	}
	return fmt.Errorf("%s: %w", failureClass, err)
}

func (s ClaimedService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validateClaimedGeneration(item workflow.WorkItem, collected CollectedGeneration) error {
	if collected.Scope.ScopeID != item.ScopeID {
		return fmt.Errorf("claimed scope_id %q produced scope_id %q", item.ScopeID, collected.Scope.ScopeID)
	}
	if collected.Scope.SourceSystem != item.SourceSystem {
		return fmt.Errorf("claimed source_system %q produced source_system %q", item.SourceSystem, collected.Scope.SourceSystem)
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

func drainHeartbeatError(errc <-chan error) error {
	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}
