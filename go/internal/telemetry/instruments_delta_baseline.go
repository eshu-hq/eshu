// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// registerProjectorDeltaBaselineFence registers the delta-baseline fence
// counter (#7319) on inst. It is separate from
// eshu_dp_superseded_generation_fence_total because a baseline refusal is an
// expected result of projector lag, not the alert-worthy #7130 signal.
func registerProjectorDeltaBaselineFence(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ProjectorDeltaBaselineFence, err = meter.Int64Counter(
		"eshu_dp_projector_delta_baseline_fence_total",
		metric.WithDescription("Projector delta-baseline fence decisions by phase (preflight, ack) and outcome (#7319)"),
	); err != nil {
		return fmt.Errorf("register ProjectorDeltaBaselineFence counter: %w", err)
	}
	return nil
}

// deltaTouchedRatioBuckets bounds the cumulative-delta touched ratio: the
// #8780 shim measured p95 0.126 on the busiest repositories, and the growth
// valve fires at 0.5. A ratio above 1 means more touched paths than base
// files (a mostly added tree).
var deltaTouchedRatioBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2}

// registerCollectorDeltaTouchedRatio registers the #8780 cumulative-delta
// growth histogram on inst. The outcome label is the closed set delta (the
// cumulative delta was used) or delta_exceeds_base_half (the growth valve took
// a full snapshot instead).
func registerCollectorDeltaTouchedRatio(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.DeltaTouchedRatio, err = meter.Float64Histogram(
		"eshu_dp_collector_delta_touched_ratio",
		metric.WithDescription("Cumulative git delta touched paths over the union base's tracked files per delta sync (#8780), "+
			"by outcome: delta (used) or delta_exceeds_base_half (the growth valve took a full snapshot)"),
		metric.WithExplicitBucketBoundaries(deltaTouchedRatioBuckets...),
	); err != nil {
		return fmt.Errorf("register DeltaTouchedRatio histogram: %w", err)
	}
	return nil
}

// AttrPhase returns a phase attribute naming the closed fence phase that
// recorded a delta-baseline decision (preflight or ack).
func AttrPhase(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPhase, v)
}
