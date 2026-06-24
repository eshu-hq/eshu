// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r *SharedProjectionRunner) recordSharedProjectionTiming(
	ctx context.Context,
	domain string,
	result PartitionProcessResult,
) {
	if r.Instruments == nil {
		return
	}
	if result.MaxIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("processed"),
			),
		)
	}
	if result.MaxBlockedIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxBlockedIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("readiness_blocked"),
			),
		)
	}
	if result.ProcessingDurationSeconds > 0 {
		r.Instruments.SharedProjectionProcessingDuration.Record(
			ctx,
			result.ProcessingDurationSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
	recordSharedProjectionStepDurations(ctx, r.Instruments, domain, result)
}

func (r *SharedProjectionRunner) recordSharedProjectionCycle(
	ctx context.Context,
	domain string,
	duration float64,
	result PartitionProcessResult,
) {
	if r.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrDomain(domain),
		)
		r.Instruments.SharedProjectionCycles.Add(ctx, 1, attrs)
		r.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
	}

	if r.Logger != nil {
		domainAttrs := telemetry.DomainAttrs(domain, "")
		logAttrs := make([]any, 0, len(domainAttrs)+1)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("intent_wait_seconds", result.MaxIntentWaitSeconds))
		logAttrs = append(logAttrs, slog.Float64("processing_duration_seconds", result.ProcessingDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("retract_duration_seconds", result.RetractDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("write_duration_seconds", result.WriteDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("mark_completed_duration_seconds", result.MarkCompletedDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("selection_duration_seconds", result.SelectionDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("lease_claim_duration_seconds", result.LeaseClaimDurationSeconds))
		logAttrs = append(logAttrs, slog.Bool("indexed_selection", result.IndexedSelection))
		logAttrs = append(logAttrs, slog.Int("unhashed_fallback_rows", result.UnhashedFallbackRows))
		logAttrs = append(logAttrs, slog.Int("terminal_no_endpoint", result.TerminalNoEndpoint))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseShared))
		r.Logger.InfoContext(ctx, "shared projection cycle completed", logAttrs...)
	}
}

func recordSharedProjectionStepDurations(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain string,
	result PartitionProcessResult,
) {
	if instruments == nil {
		return
	}
	steps := []struct {
		phase    string
		duration float64
	}{
		{phase: "retract", duration: result.RetractDurationSeconds},
		{phase: "write", duration: result.WriteDurationSeconds},
		{phase: "mark_completed", duration: result.MarkCompletedDurationSeconds},
	}
	for _, step := range steps {
		if step.duration <= 0 {
			continue
		}
		instruments.SharedProjectionStepDuration.Record(
			ctx,
			step.duration,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrWritePhase(step.phase),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
}
