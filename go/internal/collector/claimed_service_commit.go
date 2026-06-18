package collector

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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
	if collected.ValueFlowSummariesObserved || len(collected.ValueFlowSummaries) > 0 {
		if collected.FactStreamErr != nil {
			streamCommitter, ok := s.Committer.(StreamErrorFunctionSummaryClaimedCommitter)
			if !ok {
				if err := cleanupCollectedFactStream(collected); err != nil {
					return err
				}
				return errors.New("claim-aware collector committer must support fact stream errors with value-flow summaries")
			}
			return streamCommitter.CommitClaimedScopeGenerationWithStreamErrorAndFunctionSummaries(
				ctx,
				mutation,
				collected.Scope,
				collected.Generation,
				collected.Facts,
				collected.FactStreamErr,
				collected.ValueFlowSummaries,
			)
		}
		committer, ok := s.Committer.(FunctionSummaryClaimedCommitter)
		if !ok {
			if err := cleanupCollectedFactStream(collected); err != nil {
				return err
			}
			return errors.New("claim-aware collector committer must support value-flow summaries")
		}
		return committer.CommitClaimedScopeGenerationWithFunctionSummaries(
			ctx,
			mutation,
			collected.Scope,
			collected.Generation,
			collected.Facts,
			collected.ValueFlowSummaries,
		)
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
