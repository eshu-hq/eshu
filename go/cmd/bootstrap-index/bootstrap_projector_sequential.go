// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// drainProjectorSequential is the single-worker fallback. It uses the same
// per-item instrumentation as the concurrent path for consistent telemetry.
func drainProjectorSequential(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	baselineFence projector.DeltaBaselineFence,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	var completed atomic.Int64
	var failed atomic.Int64
	overallStart := time.Now()
	for {
		err := drainProjectorWorkItem(
			ctx, workSource, factStore, runner, workSink, baselineFence,
			heartbeater, heartbeatInterval,
			0, &completed, tracer, instruments, logger,
		)
		if err != nil {
			if errors.Is(err, errProjectorItemFailed) {
				failed.Add(1)
				continue
			}
			if errors.Is(err, errProjectorDrained) {
				totalFailed := failed.Load()
				if logger != nil {
					logger.InfoContext(
						ctx, "bootstrap projection complete",
						slog.Int64("items_projected", completed.Load()),
						slog.Int64("items_failed", totalFailed),
						slog.Int("workers", 1),
						slog.Float64("total_duration_seconds", time.Since(overallStart).Seconds()),
						telemetry.PhaseAttr(telemetry.PhaseProjection),
					)
				}
				if totalFailed > 0 {
					return fmt.Errorf(
						"bootstrap projection incomplete: %d work item(s) failed and were routed to retry/dead-letter; graph truth is not fully materialized",
						totalFailed,
					)
				}
				return nil
			}
			return err
		}
	}
}
