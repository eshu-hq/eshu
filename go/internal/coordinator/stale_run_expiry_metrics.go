// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// StaleRunExpiryObservation captures one run-age bound expiry pass
// (eshu-hq/eshu#10791): Outcome is success when the pass ran, ExpiredRuns
// counts non-terminal runs that aged out, ExpiredItems counts the work
// items moved to failed_terminal. A healthy deployment expires ~0 per
// pass; a sustained nonzero rate means runs are wedging faster than the
// bound clears them.
type StaleRunExpiryObservation struct {
	Outcome      string
	Duration     time.Duration
	ExpiredRuns  int
	ExpiredItems int
}

const (
	staleRunExpiryOutcomeSuccess = "success"
	staleRunExpiryOutcomeError   = "error"
)

// staleRunExpiryMetricInstruments holds the run-age bound expiry counter and
// histogram registered by newStaleRunExpiryInstruments. Splitting this pair
// out of NewMetrics keeps that function under the repo's funlen limit,
// mirroring newReapInstruments; living in its own file keeps metrics.go
// under the repo's file-length cap. Last-pass run/item counts ride the
// atomic gauges registered beside the other coordinator gauges in
// derived_metrics.go.
type staleRunExpiryMetricInstruments struct {
	total    metric.Int64Counter
	duration metric.Float64Histogram
}

// newStaleRunExpiryInstruments registers the run-age bound expiry counter
// and histogram (eshu-hq/eshu#10791).
func newStaleRunExpiryInstruments(meter metric.Meter) (staleRunExpiryMetricInstruments, error) {
	total, err := meter.Int64Counter(
		MetricPrefix+"stale_runs_expired_total",
		metric.WithDescription("Total workflow coordinator run-age bound expiry passes"),
	)
	if err != nil {
		return staleRunExpiryMetricInstruments{}, fmt.Errorf("register stale run expiry total counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		MetricPrefix+"stale_run_expiry_duration_seconds",
		metric.WithDescription("Workflow coordinator run-age bound expiry duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	)
	if err != nil {
		return staleRunExpiryMetricInstruments{}, fmt.Errorf("register stale run expiry duration histogram: %w", err)
	}
	return staleRunExpiryMetricInstruments{total: total, duration: duration}, nil
}

func (m *otelMetrics) RecordStaleRunExpiry(ctx context.Context, observation StaleRunExpiryObservation) {
	if m == nil {
		return
	}
	outcome := observation.Outcome
	if outcome != staleRunExpiryOutcomeSuccess {
		outcome = staleRunExpiryOutcomeError
	}
	attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	m.staleRunExpiryTotal.Add(ctx, 1, attrs)
	m.staleRunExpiryDuration.Record(ctx, observation.Duration.Seconds(), attrs)
	if outcome == staleRunExpiryOutcomeSuccess {
		m.staleRunExpiredRuns.Store(int64(max(observation.ExpiredRuns, 0)))
		m.staleRunExpiredItems.Store(int64(max(observation.ExpiredItems, 0)))
	}
}
