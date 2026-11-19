// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestNVDBaselineETAIsRecordedInSeconds proves the run-completion ETA gauge
// (eshu-hq/eshu#10746) reports seconds until the remaining records commit,
// and a run with no computable rate records nothing instead of a zero no
// history produced.
func TestNVDBaselineETAIsRecordedInSeconds(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselineETA(context.Background(), 14400)
	instruments.RecordNVDBaselineETA(context.Background(), 0)
	instruments.RecordNVDBaselineETA(context.Background(), -5)
	metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_eta_seconds")
	gauge, ok := metric.Data.(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatalf("ETA gauge = %T with %d points, want one Gauge[float64] point", metric.Data, len(gauge.DataPoints))
	}
	if gauge.DataPoints[0].Value != 14400 {
		t.Fatalf("ETA gauge = %v, want 14400", gauge.DataPoints[0].Value)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineETA(context.Background(), 60)
	(&Instruments{}).RecordNVDBaselineETA(context.Background(), 60)
}

// TestNVDBaselineOldestNotDoneAgeIsPhasedAndClosed proves the not-done age
// gauge (eshu-hq/eshu#10746) reports the oldest waiting chunk's age under
// its drain phase, and a phase outside the closed set collapses to other
// so a renamed planner phase never mints a label value.
func TestNVDBaselineOldestNotDoneAgeIsPhasedAndClosed(t *testing.T) {
	t.Parallel()

	want := []string{"claim_open", "projecting", "reducing", "failed"}
	if got := NVDBaselineDrainNotDonePhases(); !slices.Equal(got, want) {
		t.Fatalf("NVDBaselineDrainNotDonePhases() = %v, want %v", got, want)
	}
	phases := NVDBaselineDrainNotDonePhases()
	phases[0] = "mutated"
	if NVDBaselineDrainNotDonePhases()[0] != want[0] {
		t.Fatal("NVDBaselineDrainNotDonePhases() shares its backing array with the caller")
	}

	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselineOldestNotDoneAge(context.Background(), "projecting", 7200)
	instruments.RecordNVDBaselineOldestNotDoneAge(context.Background(), "drained", 60)
	instruments.RecordNVDBaselineOldestNotDoneAge(context.Background(), "", -30)
	metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_oldest_not_done_age_seconds")
	gauge, ok := metric.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("not-done age data = %T, want Gauge[float64]", metric.Data)
	}
	got := map[string]float64{}
	for _, point := range gauge.DataPoints {
		value, _ := point.Attributes.Value("phase")
		got[value.AsString()] = point.Value
	}
	if got["projecting"] != 7200 {
		t.Fatalf("not-done age for projecting = %v, want 7200 (all: %v)", got["projecting"], got)
	}
	if got[NVDBaselineOutcomeOther] != 0 {
		t.Fatalf("not-done age for other = %v, want one zero point from the collapsed phases (all: %v)", got[NVDBaselineOutcomeOther], got)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineOldestNotDoneAge(context.Background(), "projecting", 60)
	(&Instruments{}).RecordNVDBaselineOldestNotDoneAge(context.Background(), "projecting", 60)
}

// TestNVDBaselineDrainProbeTimeoutsAreCounted proves the drain timeout
// counter (eshu-hq/eshu#10746) sums the timeouts of one plan tick, and a
// non-positive count records nothing.
func TestNVDBaselineDrainProbeTimeoutsAreCounted(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	instruments.RecordNVDBaselineDrainProbeTimeouts(context.Background(), 2)
	instruments.RecordNVDBaselineDrainProbeTimeouts(context.Background(), 0)
	instruments.RecordNVDBaselineDrainProbeTimeouts(context.Background(), -1)
	metric := collectNVDBaselineMetric(t, reader, "eshu_dp_nvd_baseline_drain_probe_timeouts_total")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("drain timeout data = %T, want Sum[int64]", metric.Data)
	}
	var total int64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	if total != 2 {
		t.Fatalf("drain probe timeouts = %d, want 2", total)
	}
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineDrainProbeTimeouts(context.Background(), 1)
	(&Instruments{}).RecordNVDBaselineDrainProbeTimeouts(context.Background(), 1)
}
