// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ingesterDeferredRelationshipMaintenance builds the collector.Service
// AfterBatchDrained hook that drives the fleet-wide deferred-maintenance
// barrier. barrierConfig carries this shard's static identity (ShardCount,
// ShardIndex); the returned closure fills in HasCommitted per invocation from
// the hasCommitted argument collector.Service.Run passes on every drain.
// That argument is committedSinceDrain in the steady state, but Service.Run's
// once-per-process startupMaintenanceEscapeUsed latch reports true on the
// FIRST never-committed empty-batch escape call so a shard that owns no
// repositories still gets one startup maintenance pass; every later escape
// call on that shard reports false and stays join-only — it may join an
// epoch a committing shard already opened, but may not open one itself (see
// postgres.DeferredMaintenanceBarrierConfig.HasCommitted).
func ingesterDeferredRelationshipMaintenance(
	committer postgres.IngestionStore,
	barrierConfig postgres.DeferredMaintenanceBarrierConfig,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) func(context.Context, bool) error {
	return func(ctx context.Context, hasCommitted bool) error {
		shardConfig := barrierConfig
		shardConfig.HasCommitted = hasCommitted
		if err := committer.RunDeferredRelationshipMaintenanceAfterShardDrain(ctx, shardConfig, tracer, instruments); err != nil {
			if logger != nil {
				logger.ErrorContext(
					ctx, "deferred relationship maintenance failed",
					log.Err(err),
					telemetry.FailureClassAttr("deferred_relationship_maintenance_failure"),
				)
			}
			return fmt.Errorf("deferred relationship maintenance: %w", err)
		}
		return nil
	}
}
