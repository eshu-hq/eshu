package reducer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r *RepoDependencyProjectionRunner) recordRepoDependencyCycle(
	ctx context.Context,
	acceptanceUnitID string,
	rows []SharedProjectionIntentRow,
	writtenRows int,
	writtenGroups int,
	startedAt time.Time,
	timing PartitionProcessResult,
) {
	duration := time.Since(startedAt).Seconds()
	if r.Instruments != nil {
		attrs := metric.WithAttributes(telemetry.AttrDomain(DomainRepoDependency))
		r.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
		r.Instruments.CanonicalWrites.Add(ctx, int64(writtenRows), attrs)
		recordRepoDependencyStepDurations(ctx, r.Instruments, timing)
	}
	if r.Logger != nil {
		r.Logger.InfoContext(
			ctx,
			"repo dependency projection cycle completed",
			slog.String(telemetry.LogKeyAcceptanceUnitID, acceptanceUnitID),
			slog.Int("written_rows", writtenRows),
			slog.Int("written_groups", writtenGroups),
			slog.Int("processed_intents", timing.ProcessedIntents),
			slog.Int("active_intents", timing.ActiveIntents),
			slog.Int("stale_intents", timing.StaleIntents),
			slog.Int("acceptance_unit_rows", timing.AcceptanceUnitRows),
			slog.Int("replay_requests", timing.ReplayRequests),
			slog.Int("active_generations", len(uniqueGenerationIDs(rows))),
			slog.Float64("duration_seconds", duration),
			slog.Float64("processing_duration_seconds", timing.ProcessingDurationSeconds),
			slog.Float64("selection_duration_seconds", timing.SelectionDurationSeconds),
			slog.Float64("load_all_duration_seconds", timing.LoadAllDurationSeconds),
			slog.Float64("acceptance_prefetch_duration_seconds", timing.AcceptancePrefetchDurationSeconds),
			slog.Float64("retract_duration_seconds", timing.RetractDurationSeconds),
			slog.Float64("write_duration_seconds", timing.WriteDurationSeconds),
			slog.Float64("replay_duration_seconds", timing.ReplayDurationSeconds),
			slog.Float64("mark_completed_duration_seconds", timing.MarkCompletedDurationSeconds),
			slog.Float64("lease_claim_duration_seconds", timing.LeaseClaimDurationSeconds),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func recordRepoDependencyStepDurations(
	ctx context.Context,
	instruments *telemetry.Instruments,
	timing PartitionProcessResult,
) {
	steps := []struct {
		phase    string
		duration float64
	}{
		{phase: "selection", duration: timing.SelectionDurationSeconds},
		{phase: "load_all", duration: timing.LoadAllDurationSeconds},
		{phase: "acceptance_prefetch", duration: timing.AcceptancePrefetchDurationSeconds},
		{phase: "retract", duration: timing.RetractDurationSeconds},
		{phase: "write", duration: timing.WriteDurationSeconds},
		{phase: "replay", duration: timing.ReplayDurationSeconds},
		{phase: "mark_completed", duration: timing.MarkCompletedDurationSeconds},
	}
	for _, step := range steps {
		if step.duration <= 0 {
			continue
		}
		instruments.SharedProjectionStepDuration.Record(
			ctx,
			step.duration,
			metric.WithAttributes(
				telemetry.AttrDomain(DomainRepoDependency),
				telemetry.AttrWritePhase(step.phase),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
}

func (r *RepoDependencyProjectionRunner) recordRepoDependencyCycleFailure(ctx context.Context, err error, duration float64) {
	if r.Logger == nil {
		return
	}
	failureClass := "repo_dependency_projection_cycle_error"
	if IsRetryable(err) {
		failureClass = "repo_dependency_projection_retryable"
	}
	logAttrs := make([]any, 0, 6)
	for _, attr := range telemetry.DomainAttrs(string(DomainRepoDependency), "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		slog.Float64("duration_seconds", duration),
		slog.Bool("retryable", IsRetryable(err)),
		slog.String("error", err.Error()),
		telemetry.FailureClassAttr(failureClass),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
	r.Logger.ErrorContext(ctx, "repo dependency projection cycle failed", logAttrs...)
}

func (r *RepoDependencyProjectionRunner) validate() error {
	if r.IntentReader == nil {
		return errors.New("repo dependency projection runner: intent reader is required")
	}
	if r.LeaseManager == nil {
		return errors.New("repo dependency projection runner: lease manager is required")
	}
	if r.EdgeWriter == nil {
		return errors.New("repo dependency projection runner: edge writer is required")
	}
	if r.AcceptedGen == nil {
		return errors.New("repo dependency projection runner: accepted generation lookup is required")
	}
	return nil
}

func (r *RepoDependencyProjectionRunner) wait(ctx context.Context, interval time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func repoDependencyPollBackoff(base time.Duration, consecutiveEmpty int) time.Duration {
	backoff := base
	for i := 1; i < consecutiveEmpty && i < 4; i++ {
		backoff *= 2
	}
	if backoff > maxRepoDependencyPollInterval {
		backoff = maxRepoDependencyPollInterval
	}
	return backoff
}
