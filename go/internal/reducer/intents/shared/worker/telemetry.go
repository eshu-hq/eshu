// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r *Runner) recordSharedProjectionTiming(
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
	RecordStepDurations(ctx, r.Instruments, domain, result)
}

func (r *Runner) recordSharedProjectionCycle(
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

// recordSharedProjectionPartitionMetrics records the two per-(domain,
// partition_id) instruments added by #3624 Phase 1:
//
//   - eshu_dp_shared_projection_partition_processing_seconds: wall time for the
//     full ProcessPartitionOnce call (lease + select + retract + write + mark).
//     Bounded dims: domain and partition_id (0-based slot).
//   - eshu_dp_shared_projection_intents_completed_total: intents marked
//     completed this cycle, labeled by domain only, so an operator
//     can derive per-domain drain rate and pending depth.
//
// Called on every successful processPartitionWithTelemetry cycle, including
// cycles that acquired the lease but found no work (zero ProcessedIntents)
// so the histogram captures idle partition cost too.
func (r *Runner) recordSharedProjectionPartitionMetrics(
	ctx context.Context,
	domain string,
	partitionID int,
	totalDurationSeconds float64,
	result PartitionProcessResult,
) {
	if r.Instruments == nil {
		return
	}
	if totalDurationSeconds > 0 {
		r.Instruments.SharedProjectionPartitionProcessingDuration.Record(
			ctx,
			totalDurationSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrPartitionID(partitionID),
			),
		)
	}
	if result.ProcessedIntents > 0 {
		r.Instruments.SharedProjectionIntentsCompleted.Add(
			ctx,
			int64(result.ProcessedIntents),
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
			),
		)
	}
}

// recordPartitionVisit records one partition visit's outcome and the
// cell's current backoff delay (#7724): the visits counter (domain +
// closed outcome) is the idle-claim-attempt signal — every outcome except
// backoff_skipped is one lease claim — and the backoff gauge (domain +
// partition_id) shows the live per-cell delay, zero at full cadence.
func (r *Runner) recordPartitionVisit(
	ctx context.Context,
	domain string,
	partitionID int,
	outcome string,
	delay time.Duration,
) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.SharedProjectionPartitionVisits.Add(
		ctx,
		1,
		metric.WithAttributes(
			telemetry.AttrDomain(domain),
			telemetry.AttrOutcome(outcome),
		),
	)
	r.Instruments.SharedProjectionPartitionBackoff.Record(
		ctx,
		delay.Seconds(),
		metric.WithAttributes(
			telemetry.AttrDomain(domain),
			telemetry.AttrPartitionID(partitionID),
		),
	)
}

// recordSelectionPrefetch records one visit's selection prefetch behavior
// (#7724): widen rounds, and per-kind keys submitted, queries issued,
// rows returned, cache hits, and store durations. A visit that ran no
// selection (lease held, backoff skipped) records nothing.
func (r *Runner) recordSelectionPrefetch(
	ctx context.Context,
	domain string,
	result PartitionProcessResult,
) {
	if r.Instruments == nil || result.SelectionRounds == 0 {
		return
	}
	r.Instruments.SharedProjectionSelectionRounds.Add(
		ctx,
		int64(result.SelectionRounds),
		metric.WithAttributes(
			telemetry.AttrDomain(domain),
		),
	)
	r.recordPrefetchKind(ctx, domain, sharedintent.PrefetchKindAcceptance, result.PrefetchStats.Acceptance)
	r.recordPrefetchKind(ctx, domain, sharedintent.PrefetchKindReadiness, result.PrefetchStats.Readiness)
}

// recordPrefetchKind records one prefetch kind's counters and duration
// histogram, skipping zero observations so an idle kind emits no
// zero-valued points.
func (r *Runner) recordPrefetchKind(
	ctx context.Context,
	domain string,
	kind sharedintent.PrefetchKind,
	stats sharedintent.PrefetchKindStats,
) {
	attrs := metric.WithAttributes(
		telemetry.AttrDomain(domain),
		telemetry.AttrKind(prefetchKindLabel(kind)),
	)
	if stats.Keys > 0 {
		r.Instruments.SharedProjectionPrefetchKeys.Add(ctx, int64(stats.Keys), attrs)
	}
	if stats.Queries > 0 {
		r.Instruments.SharedProjectionPrefetchQueries.Add(ctx, int64(stats.Queries), attrs)
	}
	if stats.Rows > 0 {
		r.Instruments.SharedProjectionPrefetchRows.Add(ctx, int64(stats.Rows), attrs)
	}
	if stats.CacheHits > 0 {
		r.Instruments.SharedProjectionPrefetchCacheHits.Add(ctx, int64(stats.CacheHits), attrs)
	}
	if stats.Duration > 0 {
		r.Instruments.SharedProjectionPrefetchDuration.Record(ctx, stats.Duration.Seconds(), attrs)
	}
}

// prefetchKindLabel maps a sharedintent prefetch kind onto the closed
// kind label set of the prefetch instruments.
func prefetchKindLabel(kind sharedintent.PrefetchKind) string {
	switch kind {
	case sharedintent.PrefetchKindAcceptance:
		return telemetry.SharedProjectionPrefetchKindAcceptance
	case sharedintent.PrefetchKindReadiness:
		return telemetry.SharedProjectionPrefetchKindReadiness
	default:
		return string(kind)
	}
}

// RecordStepDurations is the root spelling of the
// shared-projection per-step (retract/write/mark_completed) duration
// recorder that the code-call projection runner also calls (moved here from
// the reducer root's recordSharedProjectionStepDurations, issue #6061).
func RecordStepDurations(
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
