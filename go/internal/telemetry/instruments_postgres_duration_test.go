// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestPostgresQueryDurationCountsAPlannerScanInAFiniteBucket records the
// slowest owned-package planner read measured on the coordinator (129 s,
// #9422) through the registered histogram and reads it back from an SDK
// reader. The read must land in a finite bucket at or below 300 s, not in
// +Inf, so an operator can see a scan that slows from 60 s to 120 s. The
// bucket audit table is a copy of the boundaries and cannot catch a change to
// instruments.go; this test reads the registered instrument and pins its
// bounds to the postgres-query-seconds row of
// docs/public/observability/telemetry-coverage.md, so dropping or moving any
// bucket fails here as well as in the coverage gate.
func TestPostgresQueryDurationCountsAPlannerScanInAFiniteBucket(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("postgres-duration-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	inst.PostgresQueryDuration.Record(ctx, 129)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var point *metricdata.HistogramDataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_postgres_query_duration_seconds" {
				continue
			}
			data, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(data.DataPoints) != 1 {
				t.Fatalf("%s data = %T, want one histogram point", m.Name, m.Data)
			}
			point = &data.DataPoints[0]
		}
	}
	if point == nil {
		t.Fatal("eshu_dp_postgres_query_duration_seconds was not exported")
	}
	bounds := point.Bounds
	documented := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	if !slices.Equal(bounds, documented) {
		t.Fatalf("bounds = %v, want the documented set %v", bounds, documented)
	}
	if len(bounds) == 0 || bounds[len(bounds)-1] < 300 {
		t.Fatalf("top bound = %v, want at least 300 s", bounds)
	}
	for i, upper := range bounds {
		if 129 <= upper {
			if point.BucketCounts[i] != 1 {
				t.Fatalf("bucket <= %v count = %d, want the 129 s read there", upper, point.BucketCounts[i])
			}
			if i > 0 && bounds[i-1] < 60 {
				t.Fatalf("129 s shares a bucket (%v, %v] with reads under 60 s", bounds[i-1], upper)
			}
			return
		}
	}
	t.Fatalf("129 s fell in +Inf; bounds = %v", bounds)
}
