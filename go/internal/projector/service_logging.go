package projector

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	logAttrs = append(logAttrs,
		slog.String("queue", "projector"),
		slog.String("stage", stage),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		slog.Int("worker_id", workerID),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	)
	s.Logger.InfoContext(ctx, "projector work stage completed", logAttrs...)
}

func (s Service) recordProjectionResult(ctx context.Context, work ScopeGenerationWork, start time.Time, status string, factCount int, err error, workerID int) {
	duration := time.Since(start).Seconds()

	if s.Instruments != nil {
		s.Instruments.ProjectorRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrScopeID(work.Scope.ScopeID),
		))
		s.Instruments.ProjectionsCompleted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeID(work.Scope.ScopeID),
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
	logAttrs = append(logAttrs,
		slog.String("queue", "projector"),
		slog.String("status", status),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", duration),
		slog.Int("worker_id", workerID),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	)
	if err != nil {
		logAttrs = append(logAttrs, slog.String("error", err.Error()))
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
	logAttrs = append(logAttrs,
		slog.String("queue", "projector"),
		slog.String("status", "shutdown_canceled"),
		slog.Int("fact_count", factCount),
		slog.Float64("duration_seconds", time.Since(start).Seconds()),
		slog.Int("worker_id", workerID),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		telemetry.FailureClassAttr("shutdown_canceled"),
	)
	if err != nil {
		logAttrs = append(logAttrs, slog.String("error", err.Error()))
	}
	s.Logger.InfoContext(ctx, "projector work canceled during shutdown", logAttrs...)
}
