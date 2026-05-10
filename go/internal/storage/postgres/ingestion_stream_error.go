package postgres

import (
	"context"

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
	return s.commitScopeGeneration(ctx, workflow.ClaimMutation{}, false, scopeValue, generation, factStream, factStreamErr)
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
	return s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation, factStream, factStreamErr)
}
