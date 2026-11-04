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

// TestTerraformBackendCanonicalReadDurationIsRegistered pins the #8875
// observational sensor: the canonical terraform-backends reader (and its
// filtered sibling sharing the migration-273 hot arm) must record every call
// through a dedicated duration histogram. The 15.3 s production baseline must
// land in a finite bucket at or below 300 s, not in +Inf, so a regression
// from seconds to tens of seconds stays visible week-over-week.
func TestTerraformBackendCanonicalReadDurationIsRegistered(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("terraform-backend-canonical-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	inst.TerraformBackendCanonicalReadDuration.Record(ctx, 15.3)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var point *metricdata.HistogramDataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_terraform_backend_canonical_read_duration_seconds" {
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
		t.Fatal("eshu_dp_terraform_backend_canonical_read_duration_seconds was not exported")
	}
	documented := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	if !slices.Equal(point.Bounds, documented) {
		t.Fatalf("bounds = %v, want the postgres-query-seconds set %v", point.Bounds, documented)
	}
	for i, upper := range point.Bounds {
		if 15.3 <= upper {
			if point.BucketCounts[i] != 1 {
				t.Fatalf("bucket <= %v count = %d, want the 15.3 s read there", upper, point.BucketCounts[i])
			}
			return
		}
	}
	t.Fatalf("15.3 s fell in +Inf; bounds = %v", point.Bounds)
}
