// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fixedRawRows int64

func (f fixedRawRows) ContentPolicyRawRows(context.Context) (int64, error) { return int64(f), nil }

// TestContentPolicyRawRowsGaugeObservesTheCount registers the #8833 exposure
// gauge on a real meter and reads the value its observer reports.
func TestContentPolicyRawRowsGaugeObservesTheCount(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("raw-rows-test")
	inst := &Instruments{}
	if err := RegisterContentPolicyRawRowsObservableGauge(inst, meter, fixedRawRows(7)); err != nil {
		t.Fatalf("register: %v", err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var got int64 = -1
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_content_policy_raw_rows" {
				got = m.Data.(metricdata.Gauge[int64]).DataPoints[0].Value
			}
		}
	}
	if got != 7 {
		t.Fatalf("eshu_dp_content_policy_raw_rows = %d, want 7", got)
	}
	if err := RegisterContentPolicyRawRowsObservableGauge(inst, meter, nil); err != nil {
		t.Fatalf("a nil observer must be a no-op, got %v", err)
	}
}
