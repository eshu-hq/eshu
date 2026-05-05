package reducer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r *CodeCallProjectionRunner) recordCodeCallCycle(
	ctx context.Context,
	key SharedProjectionAcceptanceKey,
	generationID string,
	writtenRows int,
	writtenGroups int,
	startedAt time.Time,
	timing PartitionProcessResult,
) error {
	duration := time.Since(startedAt).Seconds()
	if r.Instruments != nil {
		attrs := metric.WithAttributes(telemetry.AttrDomain(DomainCodeCalls))
		r.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
		r.Instruments.CanonicalWrites.Add(ctx, int64(writtenRows), attrs)
	}

	if r.Logger != nil {
		logAttrs := make([]any, 0, 11+len(telemetry.AcceptanceAttrs(key.ScopeID, key.AcceptanceUnitID, key.SourceRunID, generationID)))
		for _, attr := range telemetry.AcceptanceAttrs(key.ScopeID, key.AcceptanceUnitID, key.SourceRunID, generationID) {
			logAttrs = append(logAttrs, attr)
		}
		logAttrs = append(logAttrs,
			slog.Int("written_rows", writtenRows),
			slog.Int("written_groups", writtenGroups),
			slog.Float64("duration_seconds", duration),
			slog.Float64("intent_wait_seconds", timing.MaxIntentWaitSeconds),
			slog.Float64("blocked_intent_wait_seconds", timing.MaxBlockedIntentWaitSeconds),
			slog.Float64("processing_duration_seconds", timing.ProcessingDurationSeconds),
			slog.Float64("retract_duration_seconds", timing.RetractDurationSeconds),
			slog.Float64("write_duration_seconds", timing.WriteDurationSeconds),
			slog.Float64("mark_completed_duration_seconds", timing.MarkCompletedDurationSeconds),
			slog.Float64("selection_duration_seconds", timing.SelectionDurationSeconds),
			slog.Float64("lease_claim_duration_seconds", timing.LeaseClaimDurationSeconds),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
		r.Logger.InfoContext(ctx, "code call projection cycle completed", logAttrs...)
	}

	return nil
}

func (r *CodeCallProjectionRunner) recordCodeCallTiming(ctx context.Context, result PartitionProcessResult) {
	if r.Instruments == nil {
		return
	}
	if result.MaxIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(DomainCodeCalls),
				telemetry.AttrOutcome("processed"),
			),
		)
	}
	if result.MaxBlockedIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxBlockedIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(DomainCodeCalls),
				telemetry.AttrOutcome("readiness_blocked"),
			),
		)
	}
	if result.ProcessingDurationSeconds > 0 {
		r.Instruments.SharedProjectionProcessingDuration.Record(
			ctx,
			result.ProcessingDurationSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(DomainCodeCalls),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
	recordSharedProjectionStepDurations(ctx, r.Instruments, DomainCodeCalls, result)
}

func (r *CodeCallProjectionRunner) recordCodeCallCycleFailure(ctx context.Context, err error, duration float64) {
	if r.Logger == nil {
		return
	}

	failureClass := "code_call_projection_cycle_error"
	if IsRetryable(err) {
		failureClass = "code_call_projection_retryable"
	}

	logAttrs := make([]any, 0, 6)
	for _, attr := range telemetry.DomainAttrs(string(DomainCodeCalls), "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(logAttrs,
		slog.Float64("duration_seconds", duration),
		slog.Bool("retryable", IsRetryable(err)),
		slog.String("error", err.Error()),
		telemetry.FailureClassAttr(failureClass),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
	r.Logger.ErrorContext(ctx, "code call projection cycle failed", logAttrs...)
}

func (r *CodeCallProjectionRunner) validate() error {
	if r.IntentReader == nil {
		return errors.New("code call projection runner: intent reader is required")
	}
	if r.LeaseManager == nil {
		return errors.New("code call projection runner: lease manager is required")
	}
	if r.EdgeWriter == nil {
		return errors.New("code call projection runner: edge writer is required")
	}
	if r.AcceptedGen == nil {
		return errors.New("code call projection runner: accepted generation lookup is required")
	}
	return nil
}
