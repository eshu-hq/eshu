// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collectNVDBaselineMetric returns the one collected metric called name, or
// fails the test when the reader never saw it.
func collectNVDBaselineMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %q was never recorded", name)
	return metricdata.Metrics{}
}

// TestNVDBaselinePlanHoldReasonsAreCountedAndClosed proves the plan hold
// counter (eshu-hq/eshu#10814) is an operator-visible signal with a closed
// label: every reason the planner can give lands one data point under the
// reason label, and a value outside the set collapses to other so a typo or
// a future reason can never mint a label value. The set is the planner's
// NVDBaselineHoldReasons; the coordinator package pins the two equal.
func TestNVDBaselinePlanHoldReasonsAreCountedAndClosed(t *testing.T) {
	t.Parallel()

	want := []string{
		"baseline_chunk_in_flight",
		"baseline_projection_in_flight",
		"baseline_reducer_in_flight",
		"baseline_pace_wait",
		"baseline_retry_wait",
		"baseline_trip_open",
		"baseline_trip_unloaded",
		"baseline_trip_stale",
		"baseline_drain_unloaded",
		"baseline_downstream_failed",
		"baseline_config_error",
	}
	if got := NVDBaselinePlanHoldReasons(); !slices.Equal(got, want) {
		t.Fatalf("NVDBaselinePlanHoldReasons() = %v, want %v", got, want)
	}
	// The returned slice is a copy: a caller cannot widen the closed set.
	set := NVDBaselinePlanHoldReasons()
	set[0] = "mutated"
	if NVDBaselinePlanHoldReasons()[0] != want[0] {
		t.Fatal("NVDBaselinePlanHoldReasons() shares its backing array with the caller")
	}

	instruments, reader := testBaselineInstruments(t)
	for _, reason := range want {
		instruments.RecordNVDBaselinePlanHold(context.Background(), reason)
	}
	instruments.RecordNVDBaselinePlanHold(context.Background(), "baseline_not_a_reason")
	instruments.RecordNVDBaselinePlanHold(context.Background(), "")
	metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_plan_holds_total")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("plan holds data = %T, want Sum[int64]", metric.Data)
	}
	got := map[string]int64{}
	for _, point := range sum.DataPoints {
		value, _ := point.Attributes.Value("reason")
		got[value.AsString()] += point.Value
	}
	for _, reason := range want {
		if got[reason] != 1 {
			t.Fatalf("plan holds for %q = %d, want 1 (all: %v)", reason, got[reason], got)
		}
	}
	if got[NVDBaselineOutcomeOther] != 2 || len(got) != len(want)+1 {
		t.Fatalf("plan holds = %v, want each reason once and two unknown values collapsed to %q", got, NVDBaselineOutcomeOther)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselinePlanHold(context.Background(), want[0])
	(&Instruments{}).RecordNVDBaselinePlanHold(context.Background(), want[0])
}

// TestNVDBaselinePaceIntervalGaugeKeepsTheLastAppliedPace pins the pace gauge
// (eshu-hq/eshu#10814): it reports the latest per-year pace the planner
// applied, in seconds, and a plan that held before the pace was known (open,
// unloaded, or stale trip) passes zero, which must not overwrite the last real
// interval with a value the planner never applied.
func TestNVDBaselinePaceIntervalGaugeKeepsTheLastAppliedPace(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	read := func() float64 {
		t.Helper()
		metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_pace_interval_seconds")
		gauge, ok := metric.Data.(metricdata.Gauge[float64])
		if !ok || len(gauge.DataPoints) != 1 {
			t.Fatalf("pace gauge = %T with %d points, want one Gauge[float64] point", metric.Data, len(gauge.DataPoints))
		}
		return gauge.DataPoints[0].Value
	}
	instruments.RecordNVDBaselinePaceInterval(context.Background(), 1800)
	if got := read(); got != 1800 {
		t.Fatalf("pace gauge = %v, want 1800", got)
	}
	instruments.RecordNVDBaselinePaceInterval(context.Background(), 450)
	if got := read(); got != 450 {
		t.Fatalf("pace gauge = %v, want the latest value 450", got)
	}
	for _, skipped := range []float64{0, -1} {
		instruments.RecordNVDBaselinePaceInterval(context.Background(), skipped)
		if got := read(); got != 450 {
			t.Fatalf("pace gauge after recording %v = %v, want the last applied pace 450", skipped, got)
		}
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselinePaceInterval(context.Background(), 1800)
	(&Instruments{}).RecordNVDBaselinePaceInterval(context.Background(), 1800)
}

// TestNVDBaselineChunkCycleHistogramResolvesMinutesToHours pins the cycle
// histogram (eshu-hq/eshu#10814): a chunk's plan-to-drained time is minutes
// to hours, so the boundaries must be explicit (the default OTEL set is sized
// for milliseconds and would put every cycle in one bucket), and a
// non-positive sample records nothing.
func TestNVDBaselineChunkCycleHistogramResolvesMinutesToHours(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	for _, seconds := range []float64{240, 900, 2400, 3 * 3600, 0, -5} {
		instruments.RecordNVDBaselineChunkCycle(context.Background(), seconds)
	}
	metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_chunk_cycle_seconds")
	histogram, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("chunk cycle data = %T with %d points, want one Histogram[float64] point", metric.Data, len(histogram.DataPoints))
	}
	point := histogram.DataPoints[0]
	if point.Count != 4 || point.Sum != 240+900+2400+3*3600 {
		t.Fatalf("chunk cycle = (count %d, sum %v), want the four positive samples only", point.Count, point.Sum)
	}
	if !slices.Equal(point.Bounds, nvdBaselineChunkCycleBuckets) {
		t.Fatalf("chunk cycle bounds = %v, want the explicit set %v", point.Bounds, nvdBaselineChunkCycleBuckets)
	}
	if len(point.Bounds) < 8 || point.Bounds[0] > 60 || point.Bounds[len(point.Bounds)-1] < 6*3600 {
		t.Fatalf("chunk cycle bounds = %v, want them to span one minute to at least six hours", point.Bounds)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineChunkCycle(context.Background(), 60)
	(&Instruments{}).RecordNVDBaselineChunkCycle(context.Background(), 60)
}
