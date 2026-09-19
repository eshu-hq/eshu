// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// bootstrapAckDeferredLogEvery spaces repeated busy-scope Ack logs; with the
// Postgres queue's 2 s Ack lock timeout this is roughly one line per minute.
const bootstrapAckDeferredLogEvery = 30

// dropLostBootstrapClaim reports whether err means another attempt now owns
// the work item. The stale bootstrap worker drops the item without failing it
// or aborting the drain; the owning attempt acks or fails it. It logs the drop
// and ends span when the caller has not already ended it.
func dropLostBootstrapClaim(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	err error,
	operation string,
	span trace.Span,
	logger *slog.Logger,
) bool {
	if !errors.Is(err, projector.ErrWorkClaimLost) {
		return false
	}
	logBootstrapDrop(ctx, work, workerID, err, operation, "claim_lost",
		"projector work claim lost to another attempt", span, logger)
	return true
}

// dropDeferredBootstrapAck reports whether err is an Ack that stopped waiting
// for a busy scope, either because the drain shut down or because the
// projector.DefaultAckWaitMaxRetries bound ran out. The item is dropped; its
// lease expires and a later attempt re-projects the generation.
func dropDeferredBootstrapAck(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	err error,
	span trace.Span,
	logger *slog.Logger,
) bool {
	if !errors.Is(err, projector.ErrWorkAckDeferred) {
		return false
	}
	if ctx.Err() != nil {
		logBootstrapDrop(ctx, work, workerID, err, "ack", "shutdown_canceled",
			"projector ack abandoned at shutdown while scope was busy", span, logger)
		return true
	}
	logBootstrapDrop(ctx, work, workerID, err, "ack", "ack_wait_exhausted",
		"projector ack abandoned after waiting for busy scope", span, logger)
	return true
}

func logBootstrapDrop(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	err error,
	operation string,
	status string,
	message string,
	span trace.Span,
	logger *slog.Logger,
) {
	if span != nil {
		span.SetAttributes(attribute.String("status", status), attribute.String("operation", operation))
		span.End()
	}
	if logger == nil {
		return
	}
	logger.WarnContext(context.WithoutCancel(ctx), message,
		slog.String("scope_id", work.Scope.ScopeID), slog.String("generation_id", work.Generation.GenerationID),
		slog.String("operation", operation), slog.String("status", status),
		slog.Int("attempt_count", work.AttemptCount), slog.Int("worker_id", workerID),
		slog.String("error", err.Error()))
}

// bootstrapAckDeferredLogger logs the first busy-scope Ack deferral and then
// every bootstrapAckDeferredLogEvery retries.
func bootstrapAckDeferredLogger(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	logger *slog.Logger,
) func(int) {
	return func(retry int) {
		if logger == nil || (retry != 1 && retry%bootstrapAckDeferredLogEvery != 0) {
			return
		}
		logger.WarnContext(context.WithoutCancel(ctx), "projector ack waiting for busy scope",
			slog.String("scope_id", work.Scope.ScopeID), slog.String("generation_id", work.Generation.GenerationID),
			slog.Int("ack_retry", retry), slog.Int("attempt_count", work.AttemptCount),
			slog.Int("worker_id", workerID))
	}
}
