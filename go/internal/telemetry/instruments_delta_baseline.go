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

// AttrPhase returns a phase attribute naming the closed fence phase that
// recorded a delta-baseline decision (preflight or ack).
func AttrPhase(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPhase, v)
}
