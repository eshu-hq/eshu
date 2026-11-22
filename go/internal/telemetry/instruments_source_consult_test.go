// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSourceConsultDurationRegistersWithAlertEdgeBuckets pins the #10882
// consult-duration histogram: NewInstruments registers it, and its second-scale
// boundaries put a bucket edge exactly on the 100 ms p95 alert so the alert
// threshold is readable from the exported buckets, and run to seconds so a
// stuck consult does not land in +Inf.
func TestSourceConsultDurationRegistersWithAlertEdgeBuckets(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ReducerAdmissionSourceConsultDuration == nil {
		t.Fatal("ReducerAdmissionSourceConsultDuration not registered")
	}
	ctx := context.Background()
	instruments.ReducerAdmissionSourceConsultDuration.Record(ctx, 0.09, metric.WithAttributes(
		AttrSourceSystem("git"), AttrOutcome("ok")))

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_admission_source_consult_duration_seconds" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				t.Fatalf("histogram data = %#v, want one float64 data point", m.Data)
			}
			bounds := histogram.DataPoints[0].Bounds
			var hasAlertEdge bool
			for _, b := range bounds {
				if b == 0.1 {
					hasAlertEdge = true
				}
			}
			if !hasAlertEdge {
				t.Fatalf("bounds = %v, want a bucket edge at 0.1 (the p95 alert)", bounds)
			}
			if bounds[0] > 0.005 || bounds[len(bounds)-1] < 5 {
				t.Fatalf("bounds = %v, want from <=5ms to >=5s", bounds)
			}
			return
		}
	}
	t.Fatal("eshu_dp_reducer_admission_source_consult_duration_seconds not emitted")
}
