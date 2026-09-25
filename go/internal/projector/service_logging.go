// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector //nolint:dirgate // Root owns projector Service stage logging; service/ owns only service-catalog reducer-intent routing.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// recordWorkStage logs coarse projector service stages outside Runtime's
// ownership, especially fact loading before graph/content writes.
func (s Service) recordWorkStage(ctx context.Context, work ScopeGenerationWork, stage string, start time.Time, factCount int, workerID int) {
	if s.Logger == nil {
		return
	}
	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+5)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("projector"),
		slog.String("stage", stage),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	)
	s.Logger.InfoContext(ctx, "projector work stage completed", logAttrs...)
}

func (s Service) recordProjectionResult(ctx context.Context, work ScopeGenerationWork, start time.Time, status string, factCount int, err error, workerID int) {
	duration := time.Since(start).Seconds()

	if s.Instruments != nil {
		s.Instruments.ProjectorRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrScopeKind(string(work.Scope.ScopeKind)),
		))
		s.Instruments.ProjectionsCompleted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeKind(string(work.Scope.ScopeKind)),
			attribute.String("queue", "projector"),
			attribute.String("status", status),
		))
	}

	if s.Logger == nil {
		return
	}
	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+5)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("projector"),
		log.Status(status),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", duration),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	)
	if err != nil {
		logAttrs = append(logAttrs, log.Err(err))
		failureClass := "projection_failure"
		message := "projection failed"
		if status == "ack_failed" {
			failureClass = "ack_failure"
			message = "projection ack failed"
		}
		logAttrs = append(logAttrs, telemetry.FailureClassAttr(failureClass))
		s.Logger.ErrorContext(ctx, message, logAttrs...)
		return
	}
	s.Logger.InfoContext(ctx, "projection succeeded", logAttrs...)
}

func projectorShutdownCanceled(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s Service) recordProjectionShutdownCanceled(ctx context.Context, work ScopeGenerationWork, start time.Time, factCount int, err error, workerID int) {
	if s.Logger == nil {
		return
	}
	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	logAttrs := make([]any, 0, len(scopeAttrs)+8)
	for _, attr := range scopeAttrs {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("projector"),
		log.Status("shutdown_canceled"),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		telemetry.FailureClassAttr("shutdown_canceled"),
	)
	if err != nil {
		logAttrs = append(logAttrs, log.Err(err))
	}
	s.Logger.InfoContext(ctx, "projector work canceled during shutdown", logAttrs...)
}

// claimConflictFailureClass is the failure_class log value for a claim that
// exhausted the storage layer's bounded conflict retries.
const claimConflictFailureClass = "projector_claim_conflict"

// recoverClaimConflict reports whether err is a transient claim conflict the
// worker should survive. The storage layer already retried the claim and the
// statement rolled back, so nothing changed: the worker logs the conflict,
// waits one poll interval, and claims again. Any other claim error stays
// fatal. A wait interrupted by shutdown is reported as recovered so the
// caller's loop observes the canceled context and exits cleanly.
func (s Service) recoverClaimConflict(ctx context.Context, err error, workerID int) bool {
	if !errors.Is(err, failure.ErrWorkClaimConflict) {
		return false
	}
	if s.Logger != nil {
		s.Logger.WarnContext(ctx, "projector claim conflict; retrying after poll interval",
			slog.Int("worker_id", workerID),
			slog.String(telemetry.LogKeyFailureClass, claimConflictFailureClass),
			slog.String("error", err.Error()),
		)
	}
	_ = s.wait(ctx, s.pollInterval())
	return true
}
