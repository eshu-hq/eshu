// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceRecordsTagFactsEmittedTelemetry proves runtime telemetry records the
// tag facts added by label-backed generation output, not only resources and
// warnings.
func TestSourceRecordsTagFactsEmittedTelemetry(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("gcp-runtime-test")
	metrics, err := gcpcloud.NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {
			readFixturePage(t, "assets_list_page1.json"),
			readFixturePage(t, "assets_list_page2.json"),
		},
	})
	src := newSource(t, testConfig(testScope()), provider, nil)
	src.Metrics = metrics

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	drainFacts(t, collected)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := factsEmittedCount(rm, facts.GCPTagObservationFactKind); got != 2 {
		t.Fatalf("tag facts_emitted count = %d, want 2", got)
	}
	if got := freshnessLagSum(rm); got != 5 {
		t.Fatalf("freshness lag sum = %v, want 5", got)
	}
}

func factsEmittedCount(rm metricdata.ResourceMetrics, factKind string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_gcp_cloud_facts_emitted_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if datapointFactKind(dp) == factKind {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func datapointFactKind(dp metricdata.DataPoint[int64]) string {
	for _, attr := range dp.Attributes.ToSlice() {
		if string(attr.Key) == "fact_kind" {
			return attr.Value.AsString()
		}
	}
	return ""
}

func freshnessLagSum(rm metricdata.ResourceMetrics) float64 {
	var total float64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_gcp_cloud_freshness_lag_seconds" {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range histogram.DataPoints {
				total += dp.Sum
			}
		}
	}
	return total
}
