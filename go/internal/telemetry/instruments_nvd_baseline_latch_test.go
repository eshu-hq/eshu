// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestNVDBaselineTripLatchOutcomesAreCountedAndClosed proves the durable
// latch outcomes (eshu-hq/eshu#10768) are an operator-visible signal: the
// closed set carries the unreadable and missing-row holds that otherwise
// look like a real open trip, each outcome lands one data point under the
// outcome label, and a value outside the set collapses to other so a typo
// can never mint a label value.
func TestNVDBaselineTripLatchOutcomesAreCountedAndClosed(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		NVDBaselineTripLatchReadFailed, NVDBaselineTripLatchAbsent, NVDBaselineTripLatchLostRace,
		NVDBaselineTripLatchAppliedAfterGap, NVDBaselineTripLatchWriteFailed,
	} {
		if !slices.Contains(NVDBaselineTripLatchOutcomes(), want) {
			t.Fatalf("NVDBaselineTripLatchOutcomes() = %v, want %q in the closed set", NVDBaselineTripLatchOutcomes(), want)
		}
	}
	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselineTripLatch(context.Background(), NVDBaselineTripLatchReadFailed)
	instruments.RecordNVDBaselineTripLatch(context.Background(), "not-a-real-outcome")
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_trip_latch_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("trip latch data = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value("outcome")
				got[value.AsString()] += point.Value
			}
		}
	}
	if got[NVDBaselineTripLatchReadFailed] != 1 || got[NVDBaselineOutcomeOther] != 1 || len(got) != 2 {
		t.Fatalf("trip latch points = %v, want one %q and one %q", got, NVDBaselineTripLatchReadFailed, NVDBaselineOutcomeOther)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineTripLatch(context.Background(), NVDBaselineTripLatchApplied)
}
