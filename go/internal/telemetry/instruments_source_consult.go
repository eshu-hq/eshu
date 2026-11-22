// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// sourceConsultBuckets are second-scale boundaries for one per-source
// backpressure consult (#10882). The bounded probe reads about 92 to 101 ms at
// 10x replay scale, so the 0.05 to 0.15 edges resolve the enablement gate
// (consult p50 at most 100 ms) and the 0.1 edge is the p95 alert threshold.
// The range ends at 5 s so a stalled consult lands in a finite bucket.
var sourceConsultBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.25, 0.5, 1, 2.5, 5}

// registerSourceConsultInstruments registers the #10882 per-source
// backpressure consult-duration histogram on inst. The consult series carries
// only the closed outcome set (ok, error) plus exactly one of source_system
// (the per-source consult: admission, projector pre-write, git selection) or
// collector_kind (the family consult at a coordinator claim grant); both are
// bounded sets. No scope, generation, or work item id is a label.
func registerSourceConsultInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerAdmissionSourceConsultDuration, err = meter.Float64Histogram(
		"eshu_dp_reducer_admission_source_consult_duration_seconds",
		metric.WithDescription("Wall time of one per-source reducer backpressure consult by outcome (ok, error): the signal read or cache hit, the latch read, and any latch write. Labeled source_system for the per-source consult (ingester admission, projector pre-write, git selection) or collector_kind for the family consult at a claim grant (#10882)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sourceConsultBuckets...),
	); err != nil {
		return fmt.Errorf("register ReducerAdmissionSourceConsultDuration histogram: %w", err)
	}
	return nil
}
