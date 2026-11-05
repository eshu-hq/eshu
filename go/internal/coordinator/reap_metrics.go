// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// reapMetricInstruments holds the expired-claim reap counter and histogram
// registered by newReapInstruments. Splitting this pair out of NewMetrics
// keeps that function under the repo's funlen limit, mirroring
// newFreshnessReapInstruments; living in its own file keeps metrics.go under
// the repo's file-length cap.
type reapMetricInstruments struct {
	total    metric.Int64Counter
	duration metric.Float64Histogram
}

// newReapInstruments registers the expired-claim reap counter and histogram.
func newReapInstruments(meter metric.Meter) (reapMetricInstruments, error) {
	reapTotal, err := meter.Int64Counter(
		MetricPrefix+"reap_total",
		metric.WithDescription("Total workflow coordinator expired-claim reap passes"),
	)
	if err != nil {
		return reapMetricInstruments{}, fmt.Errorf("register reap total counter: %w", err)
	}
	reapDuration, err := meter.Float64Histogram(
		MetricPrefix+"reap_duration_seconds",
		metric.WithDescription("Workflow coordinator expired-claim reap duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return reapMetricInstruments{}, fmt.Errorf("register reap duration histogram: %w", err)
	}
	return reapMetricInstruments{total: reapTotal, duration: reapDuration}, nil
}
