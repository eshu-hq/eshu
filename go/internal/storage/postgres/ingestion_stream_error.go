package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// CommitScopeGenerationWithStreamError persists one scope generation and fails
// the transaction if factStreamErr reports a producer error after facts close.
func (s IngestionStore) CommitScopeGenerationWithStreamError(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, factStreamErr, nil)
}

// CommitClaimedScopeGenerationWithStreamError persists one claimed generation
// and fails the same transaction if factStreamErr reports a producer error
// after facts close.
func (s IngestionStore) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	if err := validateClaimMutation(mutation); err != nil {
		drainFacts(factStream)
		return err
	}
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, factStreamErr, nil)
}

// CommitScopeGenerationWithStreamErrorAndFunctionSummaries persists one scope
// generation, fails the transaction on an asynchronous fact stream error, and
// recomposes value-flow summaries in the same durable boundary.
func (s IngestionStore) CommitScopeGenerationWithStreamErrorAndFunctionSummaries(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	summaries []collector.ValueFlowSummarySnapshot,
) error {
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, factStreamErr, summaries)
}

// CommitClaimedScopeGenerationWithStreamErrorAndFunctionSummaries persists one
// claimed generation with summaries and fails the same transaction if the fact
// stream reports an asynchronous producer error.
func (s IngestionStore) CommitClaimedScopeGenerationWithStreamErrorAndFunctionSummaries(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
	summaries []collector.ValueFlowSummarySnapshot,
) error {
	if err := validateClaimMutation(mutation); err != nil {
		drainFacts(factStream)
		return err
	}
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, factStreamErr, summaries)
}
