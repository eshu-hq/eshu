package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func ingesterDeferredRelationshipMaintenance(
	committer postgres.IngestionStore,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := committer.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
			if logger != nil {
				logger.ErrorContext(ctx, "deferred relationship backfill failed",
					slog.String("error", err.Error()),
					telemetry.FailureClassAttr("backfill_deferred_failure"),
				)
			}
			return fmt.Errorf("deferred relationship backfill: %w", err)
		}
		if err := committer.ReopenDeploymentMappingWorkItems(ctx, tracer, instruments); err != nil {
			if logger != nil {
				logger.ErrorContext(ctx, "reopen deployment_mapping work items failed",
					slog.String("error", err.Error()),
					telemetry.FailureClassAttr("reopen_deployment_mapping_failure"),
				)
			}
			return fmt.Errorf("reopen deployment_mapping work items: %w", err)
		}
		return nil
	}
}
