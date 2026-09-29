// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// preflightBootstrapDeltaBaseline runs the shared #7319 delta-baseline fence
// before the bootstrap projector starts its heartbeat or loads facts, and
// reports whether it handled the item. The shared function logs and counts a
// refusal. A refused delta and a lost claim are dropped without Fail; a fence
// that could not decide routes the retryable cause to the queue's Fail path
// through the same isolation as a projection failure, without projecting.
func preflightBootstrapDeltaBaseline(
	ctx context.Context,
	fence projector.DeltaBaselineFence,
	workSink projector.ProjectorWorkSink,
	work projector.ScopeGenerationWork,
	workerID int,
	itemStart time.Time,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (bool, error) {
	err := projector.PreflightDeltaBaseline(ctx, fence, work, instruments, logger)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, failure.ErrWorkSuperseded):
		endBootstrapDeltaSpan(span, projector.DeltaBaselineMismatchClass)
		return true, nil
	case dropLostBootstrapClaim(ctx, work, workerID, err, "delta_baseline", span, logger):
		return true, nil
	default:
		return true, isolateBootstrapProjectorFailure(ctx, workSink, work, workerID, err, span, logger, func() {
			recordBootstrapProjectionResult(ctx, work, workerID, itemStart, "failed", 0, err, span, instruments, logger)
		})
	}
}

// endRefusedBootstrapDelta ends the item span for an Ack-phase delta-baseline
// refusal and reports whether ackErr was one. The Postgres queue already
// logged it at ERROR and counted it, so the generic "superseded by newer
// generation" outcome, which would be false here, is skipped.
func endRefusedBootstrapDelta(ackErr error, span trace.Span) bool {
	if !projector.IsDeltaBaselineRefusal(ackErr) {
		return false
	}
	endBootstrapDeltaSpan(span, projector.DeltaBaselineMismatchAfterProjectionClass)
	return true
}

// endBootstrapDeltaSpan marks the item span as a delta-baseline refusal.
func endBootstrapDeltaSpan(span trace.Span, failureClass string) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.String("status", "superseded"),
		attribute.String(telemetry.LogKeyFailureClass, failureClass),
	)
	span.End()
}
