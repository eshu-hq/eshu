package main

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// gcpStatusCommitter wraps the durable collector committer and records the GCP
// claim lifecycle outcome on the bounded claim counter when a generation is
// committed. It mirrors the awscloud status committer shape so facts and
// generation state commit atomically through the inner committer while the
// collector records committed/failed status.
//
// This slice has no GCP scan-status store (that is reducer-owned and deferred),
// so the committer records only the bounded claim metric. It never logs or
// labels resource identity, project ids, or credential names.
type gcpStatusCommitter struct {
	inner   collector.Committer
	metrics *gcpcloud.Metrics
}

// newGCPStatusCommitter wraps inner so commit outcomes record claim status.
func newGCPStatusCommitter(inner collector.Committer, metrics *gcpcloud.Metrics) gcpStatusCommitter {
	return gcpStatusCommitter{inner: inner, metrics: metrics}
}

// CommitScopeGeneration commits the generation through the inner committer and
// records the bounded claim outcome.
func (c gcpStatusCommitter) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	err := c.inner.CommitScopeGeneration(ctx, scopeValue, generation, factStream)
	c.recordOutcome(ctx, err)
	return err
}

func (c gcpStatusCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	committer, ok := c.inner.(collector.ClaimedCommitter)
	if !ok {
		return errors.New("inner GCP committer must implement ClaimedCommitter")
	}
	err := committer.CommitClaimedScopeGeneration(ctx, mutation, scopeValue, generation, factStream)
	c.recordOutcome(ctx, err)
	return err
}

func (c gcpStatusCommitter) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	committer, ok := c.inner.(collector.StreamErrorClaimedCommitter)
	if !ok {
		return errors.New("inner GCP committer must implement StreamErrorClaimedCommitter")
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

func (c gcpStatusCommitter) recordOutcome(ctx context.Context, commitErr error) {
	if c.metrics == nil {
		return
	}
	if commitErr != nil {
		c.metrics.RecordClaim(ctx, gcpcloud.ClaimStatusFailed)
		return
	}
	c.metrics.RecordClaim(ctx, gcpcloud.ClaimStatusSucceeded)
}

var (
	_ collector.Committer                   = gcpStatusCommitter{}
	_ collector.ClaimedCommitter            = gcpStatusCommitter{}
	_ collector.StreamErrorClaimedCommitter = gcpStatusCommitter{}
)
