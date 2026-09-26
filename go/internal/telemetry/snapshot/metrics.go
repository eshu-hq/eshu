// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// refreshMetrics holds the operator-facing signals for the refresh loops:
// what each refresh did, how long it took, and how old the served snapshot is.
type refreshMetrics struct {
	refreshes metric.Int64Counter
	duration  metric.Float64Histogram
}

func newRefreshMetrics(meter metric.Meter, r *Refresher) (*refreshMetrics, error) {
	refreshes, err := meter.Int64Counter(
		"eshu_dp_gauge_snapshot_refreshes_total",
		metric.WithDescription("Total background refreshes of graph- and Postgres-backed observable-gauge snapshots by gauge and outcome (success, error, timeout)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register gauge snapshot refreshes counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		"eshu_dp_gauge_snapshot_refresh_duration_seconds",
		metric.WithDescription("Duration of each background graph- and Postgres-backed observable-gauge snapshot refresh by gauge and outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120),
	)
	if err != nil {
		return nil, fmt.Errorf("register gauge snapshot refresh duration histogram: %w", err)
	}
	if _, err := meter.Float64ObservableGauge(
		"eshu_dp_gauge_snapshot_age_seconds",
		metric.WithDescription("Age in seconds of the snapshot a graph- or Postgres-backed observable gauge currently serves; absent until the first successful refresh"),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			r.observeAges(o)
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("register gauge snapshot age gauge: %w", err)
	}
	return &refreshMetrics{refreshes: refreshes, duration: duration}, nil
}

func (m *refreshMetrics) record(ctx context.Context, gauge, outcome string, elapsed time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionGauge, gauge),
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	)
	m.refreshes.Add(ctx, 1, attrs)
	m.duration.Record(ctx, elapsed.Seconds(), attrs)
}

// observeAges reports each published snapshot's age. It reads only the cached
// snapshot pointers, so it never blocks the metrics collection it runs on.
func (r *Refresher) observeAges(o metric.Float64Observer) {
	r.mu.Lock()
	sources := append([]*Source(nil), r.sources...)
	r.mu.Unlock()
	now := r.now()
	for _, source := range sources {
		current := source.latest.Load()
		if current == nil {
			continue
		}
		o.Observe(now.Sub(current.at).Seconds(), metric.WithAttributes(attribute.String(telemetry.MetricDimensionGauge, source.name)))
	}
}
