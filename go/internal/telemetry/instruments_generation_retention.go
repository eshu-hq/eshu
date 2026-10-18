// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// generationRetentionPhaseBuckets span a sub-millisecond catalog check to a
// multi-minute prune. A phase or lock hold past 10 s is the #7279 symptom: a
// concurrent fact insert into a locked scope waits that long.
var generationRetentionPhaseBuckets = []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60, 300}

// generationRetentionSearchIndexBatchBuckets span one pass's search index
// pre-delete batch count (#7427): two batches drain an empty generation's
// tables, and the default 2,000,000-row cap is about 100 full batches.
var generationRetentionSearchIndexBatchBuckets = []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}

// SpanGenerationRetentionExpiredReport wraps one expired-generations
// live-signal report read (#7585 PR3): the lock-free SELECT plus the
// in-memory evaluation. The span carries the report counts; each
// expired-but-blocked generation adds one
// generation_retention.expired_blocked event carrying its scope_id,
// generation_id, reason, and age. Scope and generation identifiers stay
// in span events and structured logs, never in metric labels.
const SpanGenerationRetentionExpiredReport = "generation_retention.expired_report"

// GenerationRetentionExpiredBlockedEvent is the span event one
// expired-but-blocked generation adds to the expired-report span.
const GenerationRetentionExpiredBlockedEvent = "generation_retention.expired_blocked"

// registerGenerationRetentionPhaseInstruments registers the per-phase,
// scope-lock-hold and search index pre-delete batch histograms of the
// generation retention cycle, and the search index orphan census gauge and
// swept-rows counter (#7427 PR2), on inst.
func registerGenerationRetentionPhaseInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.GenerationRetentionPhaseDuration, err = meter.Float64Histogram(
		"eshu_dp_generation_retention_phase_duration_seconds",
		metric.WithDescription("Generation retention transaction time by bounded phase"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(generationRetentionPhaseBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionPhaseDuration histogram: %w", err)
	}
	if inst.GenerationRetentionScopeLockHold, err = meter.Float64Histogram(
		"eshu_dp_generation_retention_scope_lock_hold_seconds",
		metric.WithDescription("Time one generation retention transaction held its ingestion scope row locks"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(generationRetentionPhaseBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionScopeLockHold histogram: %w", err)
	}
	if inst.GenerationRetentionSearchIndexBatches, err = meter.Int64Histogram(
		"eshu_dp_generation_retention_search_index_batches",
		metric.WithDescription("Search index pre-delete batch transactions one generation retention pass committed before its prune"),
		metric.WithUnit("{batch}"),
		metric.WithExplicitBucketBoundaries(generationRetentionSearchIndexBatchBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionSearchIndexBatches histogram: %w", err)
	}
	if inst.SearchIndexOrphanGenerations, err = meter.Int64Gauge(
		"eshu_dp_search_index_orphan_generations",
		metric.WithDescription("Search index (scope_id, generation_id) pairs whose scope_generations row is gone, by table, at the last orphan census"),
		metric.WithUnit("{generation}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexOrphanGenerations gauge: %w", err)
	}
	if inst.SearchIndexOrphanRowsSwept, err = meter.Int64Counter(
		"eshu_dp_search_index_orphan_rows_swept_total",
		metric.WithDescription("Search index rows the orphan sweep deleted because their generation no longer exists, by table"),
		metric.WithUnit("{row}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexOrphanRowsSwept counter: %w", err)
	}
	return nil
}

// registerGenerationRetentionExpiredReportInstruments registers the
// expired-generations live-signal report instruments (#7585 PR3) on inst:
// the expired-but-blocked counter by closed blocked reason and the report
// duration histogram. The report's failures reuse the shared
// GenerationRetentionFailures counter with the expired_report_error reason,
// so no new failure instrument is registered here.
func registerGenerationRetentionExpiredReportInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.GenerationRetentionExpiredBlocked, err = meter.Int64Counter(
		"eshu_dp_generation_retention_expired_blocked_total",
		metric.WithDescription("Expired superseded generations retained by a safety exception, by closed blocked reason (active_current, live_work, retrying, dependency_pin)"),
		metric.WithUnit("{generation}"),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionExpiredBlocked counter: %w", err)
	}
	if inst.GenerationRetentionExpiredReportDuration, err = meter.Float64Histogram(
		"eshu_dp_generation_retention_expired_report_duration_seconds",
		metric.WithDescription("One expired-generations live-signal report read: the lock-free SELECT plus the in-memory evaluation"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(generationRetentionPhaseBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionExpiredReportDuration histogram: %w", err)
	}
	return nil
}
