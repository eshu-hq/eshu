// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// CounterValue returns the value of the int64-sum data point of metricName
// whose attributes equal wantAttrs, failing the test when the metric or the
// data point is absent. It mirrors the reducer root's test helper of the same
// shape (internal/reducer/service_ack_observability_test.go); Go test files
// cannot share unexported symbols across a package boundary, so the leaf test
// packages import it from here instead of duplicating it per leaf (#7648).
func CounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, m.Data)
			}

			for _, dp := range sum.DataPoints {
				if HasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

// GaugeValue returns the value of the int64-gauge data point of name whose
// attributes equal want, failing the test when the metric or the data point
// is absent. It is the gauge counterpart of CounterValue, shared by the
// obligation and producer census tests (#7648).
func GaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want int64 gauge", name, m.Data)
			}
			for _, dp := range gauge.DataPoints {
				if HasAttrs(dp.Attributes.ToSlice(), want) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("gauge %s with attrs %v not found", name, want)
	return 0
}

// HasAttrs reports whether actual carries exactly the want attributes, no more
// and no fewer. Leaf tests use it directly for histogram data points, which
// neither CounterValue's nor GaugeValue's read can reach.
func HasAttrs(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}

	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}

	return true
}
