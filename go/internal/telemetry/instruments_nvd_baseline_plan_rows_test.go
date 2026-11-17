// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// planRowDataPoints collects eshu_dp_nvd_baseline_plan_rows_total by outcome.
func planRowDataPoints(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	points := make(map[string]int64)
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_plan_rows_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want an int64 sum", metric.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(AttrOutcome("").Key)
				points[value.AsString()] += point.Value
			}
		}
	}
	return points
}

// TestNVDBaselinePlanRowsCountsRecordedAndNotInserted pins the #10828 signal:
// one counter, one closed outcome label, and the two outcomes an admission can
// split its plans into. The counts are added whole, so one admission is one
// call and not one call per row.
func TestNVDBaselinePlanRowsCountsRecordedAndNotInserted(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	if instruments.NVDBaselinePlanRows == nil {
		t.Fatal("NVDBaselinePlanRows = nil, want the plan row counter registered")
	}
	ctx := context.Background()
	instruments.RecordNVDBaselinePlanRows(ctx, 3, 2)
	instruments.RecordNVDBaselinePlanRows(ctx, 1, 0)

	got := planRowDataPoints(t, reader)
	if got[NVDBaselinePlanRowRecorded] != 4 || got[NVDBaselinePlanRowNotInserted] != 2 || len(got) != 2 {
		t.Fatalf("points = %v, want recorded=4 not_inserted=2 and no other label value", got)
	}
}

// TestNVDBaselinePlanRowsOutcomeSetIsClosed keeps the label bounded: exactly two
// outcomes, no run, year, or scope ever becoming a label, and a zero or negative
// count adding no series (a zero-valued series would read as "the write path
// ran" on a tick where it did not).
func TestNVDBaselinePlanRowsOutcomeSetIsClosed(t *testing.T) {
	t.Parallel()

	want := []string{"recorded", "not_inserted"}
	got := NVDBaselinePlanRowOutcomes()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("NVDBaselinePlanRowOutcomes() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if NVDBaselinePlanRowOutcomes()[0] != "recorded" {
		t.Fatal("NVDBaselinePlanRowOutcomes() returned the backing array; callers must get a copy")
	}

	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselinePlanRows(context.Background(), 0, -4)
	if points := planRowDataPoints(t, reader); len(points) != 0 {
		t.Fatalf("points = %v, want no series for zero or negative counts", points)
	}
}

// TestNVDBaselinePlanRowsIsNilSafe: telemetry must never fail an admission.
func TestNVDBaselinePlanRowsIsNilSafe(t *testing.T) {
	t.Parallel()

	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselinePlanRows(context.Background(), 1, 1)
	(&Instruments{}).RecordNVDBaselinePlanRows(context.Background(), 1, 1)
}
