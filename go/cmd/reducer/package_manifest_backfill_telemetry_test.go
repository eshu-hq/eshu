// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestPackageManifestBackfillPassTelemetry(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	ctx := context.Background()
	for _, pass := range []struct {
		ran   bool
		ready bool
		err   error
		want  string
	}{
		{ran: false, want: "contended"},
		{ran: true, err: errors.New("write failed"), want: "failed"},
		{ran: true, want: "incomplete"},
		{ran: true, ready: true, want: "ready"},
	} {
		recordPackageManifestBackfillPass(ctx, inst, pass.ran, pass.ready, pass.err, time.Second)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := make(map[string]int64)
	var durationCount uint64
	var lastSuccess, readyGauge bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_package_manifest_backfill_duration_seconds":
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("duration metric has type %T", m.Data)
				}
				for _, point := range histogram.DataPoints {
					durationCount += point.Count
				}
				continue
			case "eshu_dp_package_manifest_backfill_last_success_unixtime":
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || len(gauge.DataPoints) != 1 {
					t.Fatalf("last-success metric has type %T or unexpected points", m.Data)
				}
				lastSuccess = gauge.DataPoints[0].Value > 0
				continue
			case "eshu_dp_package_manifest_backfill_ready":
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || len(gauge.DataPoints) != 1 {
					t.Fatalf("ready metric has type %T or unexpected points", m.Data)
				}
				readyGauge = gauge.DataPoints[0].Value == 1
				continue
			case "eshu_dp_package_manifest_backfill_passes_total":
			default:
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("pass metric has type %T", m.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == telemetry.MetricDimensionOutcome {
						got[attr.Value.AsString()] += point.Value
					}
				}
			}
		}
	}
	if durationCount != 4 || !lastSuccess || !readyGauge {
		t.Errorf("duration count=%d lastSuccess=%t readyGauge=%t, want 4/true/true", durationCount, lastSuccess, readyGauge)
	}
	for _, outcome := range []string{"contended", "failed", "incomplete", "ready"} {
		if got[outcome] != 1 {
			t.Errorf("outcome %s = %d, want 1", outcome, got[outcome])
		}
	}
}
