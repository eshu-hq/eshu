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
	barrierConfig postgres.DeferredMaintenanceBarrierConfig,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := committer.RunDeferredRelationshipMaintenanceAfterShardDrain(ctx, barrierConfig, tracer, instruments); err != nil {
			if logger != nil {
				logger.ErrorContext(
					ctx, "deferred relationship maintenance failed",
					slog.String("error", err.Error()),
					telemetry.FailureClassAttr("deferred_relationship_maintenance_failure"),
				)
			}
			return fmt.Errorf("deferred relationship maintenance: %w", err)
		}
		return nil
	}
}
