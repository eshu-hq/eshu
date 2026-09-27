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

// registerGenerationRetentionPhaseInstruments registers the per-phase and
// scope-lock-hold histograms of the generation retention cycle on inst.
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
	return nil
}
