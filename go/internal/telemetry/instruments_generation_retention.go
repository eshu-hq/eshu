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

// generationRetentionPredrainedRowBuckets span one pass's predrained rows
// for generations it did not prune (#10677): a single bounded page (about
// 100 rows) up to a full row-cap drain (500,000 rows by default).
var generationRetentionPredrainedRowBuckets = []float64{1, 10, 100, 1000, 10000, 100000, 500000, 1000000}

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

// SpanGenerationRetentionRelationshipSweep wraps one relationship retention
// pass (#10823): the orphan census and, when deletes are enabled, the committed
// batches. The span carries the pass counts; each committed batch adds one
// generation_retention.relationship_batch event with its row counts. Generation
// identifiers stay out of span attributes and metric labels.
const SpanGenerationRetentionRelationshipSweep = "generation_retention.relationship_sweep"

// GenerationRetentionRelationshipBatchEvent is the span event one committed
// relationship retention batch adds to the sweep span.
const GenerationRetentionRelationshipBatchEvent = "generation_retention.relationship_batch"

// registerGenerationRetentionPhaseInstruments registers the per-phase,
// scope-lock-hold and search index pre-delete batch histograms of the
// generation retention cycle, the search index orphan census gauge and
// swept-rows counter (#7427 PR2), and the relationship retention census gauge
// and sweep counters (#10823), on inst.
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
	if inst.GenerationRetentionChunkPredeleteBatches, err = meter.Int64Histogram(
		"eshu_dp_generation_retention_chunk_predelete_batches",
		metric.WithDescription("Chunk pre-delete batch transactions one generation retention pass committed before its prune"),
		metric.WithUnit("{batch}"),
		metric.WithExplicitBucketBoundaries(generationRetentionSearchIndexBatchBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionChunkPredeleteBatches histogram: %w", err)
	}
	if inst.GenerationRetentionPredrainedOverLimitGenerations, err = meter.Int64Histogram(
		"eshu_dp_generation_retention_predrained_over_limit_generations",
		metric.WithDescription("Drain members that exceeded the row limit on own facts at the predrain read, per generation retention pass"),
		metric.WithUnit("{generation}"),
		metric.WithExplicitBucketBoundaries(generationRetentionSearchIndexBatchBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionPredrainedOverLimitGenerations histogram: %w", err)
	}
	if inst.GenerationRetentionPredrainedSkippedRows, err = meter.Int64Histogram(
		"eshu_dp_generation_retention_predrained_skipped_rows",
		metric.WithDescription("Fact and content rows the chunk pre-delete drained for generations one retention pass did not prune, per generation retention pass"),
		metric.WithUnit("{row}"),
		metric.WithExplicitBucketBoundaries(generationRetentionPredrainedRowBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionPredrainedSkippedRows histogram: %w", err)
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
	if inst.SearchIndexSupersededRowsSwept, err = meter.Int64Counter(
		"eshu_dp_search_index_superseded_rows_swept_total",
		metric.WithDescription("Search index rows the superseded sweep deleted from a still-existing superseded generation whose facts stay held, by table"),
		metric.WithUnit("{row}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexSupersededRowsSwept counter: %w", err)
	}
	if inst.SearchIndexPartitionBytes, err = meter.Int64Gauge(
		"eshu_dp_search_index_partition_bytes",
		metric.WithDescription("Search index terms leaf size in bytes, by partition (p00-p63) and component (heap, index, total), at the last size census"),
		metric.WithUnit("{byte}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexPartitionBytes gauge: %w", err)
	}
	if inst.SearchIndexPartitionEstimatedRows, err = meter.Int64Gauge(
		"eshu_dp_search_index_partition_estimated_rows",
		metric.WithDescription("Search index terms leaf estimated live rows (n_live_tup when positive, else reltuples), by partition (p00-p63), at the last size census"),
		metric.WithUnit("{row}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexPartitionEstimatedRows gauge: %w", err)
	}
	if inst.SearchIndexPartitionBytesPerRow, err = meter.Float64Gauge(
		"eshu_dp_search_index_partition_bytes_per_row",
		metric.WithDescription("Search index terms leaf bytes per estimated row, by partition (p00-p63) and component (total, heap, index); the WARN/PAGE suspect thresholds read this series"),
		metric.WithUnit("{byte}"),
	); err != nil {
		return fmt.Errorf("register SearchIndexPartitionBytesPerRow gauge: %w", err)
	}
	if inst.RelationshipRetentionOrphanGenerations, err = meter.Int64Gauge(
		"eshu_dp_relationship_retention_orphan_generations",
		metric.WithDescription("Distinct relationship generations no scope_generations row, active generation row or scope active pointer keeps alive, by table, at the last census (saturates at the census list limit)"),
		metric.WithUnit("{generation}"),
	); err != nil {
		return fmt.Errorf("register RelationshipRetentionOrphanGenerations gauge: %w", err)
	}
	if inst.RelationshipRetentionRowsSwept, err = meter.Int64Counter(
		"eshu_dp_relationship_retention_rows_swept_total",
		metric.WithDescription("Relationship rows the retention sweep deleted from orphan generations, by table"),
		metric.WithUnit("{row}"),
	); err != nil {
		return fmt.Errorf("register RelationshipRetentionRowsSwept counter: %w", err)
	}
	if inst.RelationshipRetentionGenerationsSkipped, err = meter.Int64Counter(
		"eshu_dp_relationship_retention_generations_skipped_total",
		metric.WithDescription("Orphan relationship generations a sweep pass left for a later pass, by reason (lock_busy, lock_timeout, ineligible)"),
		metric.WithUnit("{generation}"),
	); err != nil {
		return fmt.Errorf("register RelationshipRetentionGenerationsSkipped counter: %w", err)
	}
	if inst.RelationshipRetentionCapped, err = meter.Int64Counter(
		"eshu_dp_relationship_retention_capped_total",
		metric.WithDescription("Relationship retention sweep passes that stopped with orphans left, by cap (rows, deadline, list)"),
		metric.WithUnit("{pass}"),
	); err != nil {
		return fmt.Errorf("register RelationshipRetentionCapped counter: %w", err)
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
