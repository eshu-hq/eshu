// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func claimedHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string) uint64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			var count uint64
			for _, point := range histogram.DataPoints {
				count += point.Count
			}
			return count
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return 0
}

func claimedHistogramHasAttr(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	key string,
) bool {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == key {
						return true
					}
				}
			}
			return false
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return false
}
