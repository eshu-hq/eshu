// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// assertInt64CounterValue, assertFloat64HistogramCount, assertInt64HistogramCount,
// assertMetricMissing and attributesMatch duplicate the telemetry assertion
// helpers from the edge/writer leaf (edge_writer_telemetry_test.go). Test
// helpers cannot be imported across the package split, so each side carries
// its own copy.

func assertInt64CounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	wantValue int64,
) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, wantAttrs) {
					if got := point.Value; got != wantValue {
						t.Fatalf("metric %s value = %d, want %d", metricName, got, wantValue)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
}

func assertFloat64HistogramCount(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	wantCount uint64,
) {
	t.Helper()

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetric.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributesMatch(point.Attributes, wantAttrs) {
					if got := point.Count; got != wantCount {
						t.Fatalf("metric %s count = %d, want %d", metricName, got, wantCount)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
}

func attributesMatch(attrs attribute.Set, want map[string]string) bool {
	if len(want) == 0 {
		return len(attrs.ToSlice()) == 0
	}

	gotAttrs := attrs.ToSlice()
	if len(gotAttrs) != len(want) {
		return false
	}
	for _, attr := range gotAttrs {
		wantValue, ok := want[string(attr.Key)]
		if !ok || attr.Value.AsString() != wantValue {
			return false
		}
	}
	return true
}

// TestEdgeWriterArtifactPhaseLogsOneSummaryEntry pins the #6730 owner
// finding: one claim with k artifact statements must emit ONE
