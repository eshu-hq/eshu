// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestNVDBaselineTripCloseRefusalIsACountedTransition proves an early close
// (eshu-hq/eshu#10760) is an operator-visible signal: the refusal is part
// of the closed transition set, and recording it lands one data point under
// the close_refused label on the shared trip-transition counter.
func TestNVDBaselineTripCloseRefusalIsACountedTransition(t *testing.T) {
	t.Parallel()

	if !slices.Contains(NVDBaselineTripTransitions(), NVDBaselineTripCloseRefused) {
		t.Fatalf("NVDBaselineTripTransitions() = %v, want %q in the closed set", NVDBaselineTripTransitions(), NVDBaselineTripCloseRefused)
	}
	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselineTripTransition(context.Background(), NVDBaselineTripCloseRefused)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_trip_transitions_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("trip transitions data = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value("transition")
				got[value.AsString()] += point.Value
			}
		}
	}
	if got[NVDBaselineTripCloseRefused] != 1 || len(got) != 1 {
		t.Fatalf("trip transition points = %v, want exactly one %q", got, NVDBaselineTripCloseRefused)
	}
}
