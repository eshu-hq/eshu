package main

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// azureStatusCommitter wraps the durable collector committer and records the
// Azure claim lifecycle outcome on the bounded claim counter when a generation
// is committed. It mirrors the gcpcloud status committer shape so facts and
// generation state commit atomically through the inner committer while the
// collector records committed/failed status.
//
// This slice has no Azure scan-status store (that is reducer-owned and
// deferred), so the committer records only the bounded claim metric. It never
// logs or labels resource identity, subscription/tenant ids, or credential
// names.
type azureStatusCommitter struct {
	inner   collector.Committer
	metrics azurecloud.Metrics
}

// newAzureStatusCommitter wraps inner so commit outcomes record claim status.
func newAzureStatusCommitter(inner collector.Committer, metrics azurecloud.Metrics) azureStatusCommitter {
	return azureStatusCommitter{inner: inner, metrics: metrics}
}

// CommitScopeGeneration commits the generation through the inner committer and
// records the bounded claim outcome.
func (c azureStatusCommitter) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	err := c.inner.CommitScopeGeneration(ctx, scopeValue, generation, factStream)
	c.recordOutcome(ctx, err)
	return err
}

func (c azureStatusCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	committer, ok := c.inner.(collector.ClaimedCommitter)
	if !ok {
		return errors.New("inner azure committer must implement ClaimedCommitter")
	}
	err := committer.CommitClaimedScopeGeneration(ctx, mutation, scopeValue, generation, factStream)
	c.recordOutcome(ctx, err)
	return err
}

func (c azureStatusCommitter) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	committer, ok := c.inner.(collector.StreamErrorClaimedCommitter)
	if !ok {
		return errors.New("inner azure committer must implement StreamErrorClaimedCommitter")
	}
	err := committer.CommitClaimedScopeGenerationWithStreamError(
		ctx,
		mutation,
		scopeValue,
		generation,
		factStream,
		factStreamErr,
	)
	c.recordOutcome(ctx, err)
	return err
}

func (c azureStatusCommitter) recordOutcome(ctx context.Context, commitErr error) {
	if c.metrics == nil {
		return
	}
	if commitErr != nil {
		c.metrics.RecordClaim(ctx, azurecloud.ClaimStatusFailed)
		return
	}
	c.metrics.RecordClaim(ctx, azurecloud.ClaimStatusSucceeded)
}

var (
	_ collector.Committer                   = azureStatusCommitter{}
	_ collector.ClaimedCommitter            = azureStatusCommitter{}
	_ collector.StreamErrorClaimedCommitter = azureStatusCommitter{}
)
