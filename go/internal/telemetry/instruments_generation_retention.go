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

// registerGenerationRetentionPhaseInstruments registers the per-phase,
// scope-lock-hold and search index pre-delete batch histograms of the
// generation retention cycle on inst.
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
		metric.WithDescription("Search index pre-delete batch transactions one generation retention pass ran before its prune"),
		metric.WithUnit("{batch}"),
		metric.WithExplicitBucketBoundaries(generationRetentionSearchIndexBatchBuckets...),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionSearchIndexBatches histogram: %w", err)
	}
	return nil
}
