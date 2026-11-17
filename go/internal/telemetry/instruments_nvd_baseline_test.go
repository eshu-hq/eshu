// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func testBaselineInstruments(t *testing.T) (*Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func collectedCounterNames(t *testing.T, reader *sdkmetric.ManualReader) map[string]bool {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	names := make(map[string]bool)
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			names[metric.Name] = true
		}
	}
	return names
}

// TestInstrumentsRegisterNVDBaselineSignals is the R8 registration cell:
// the baseline advance counter, plan counter, trip counter, and dirty
// gauges exist after construction, so emission sites never record into a
// nil meter.
func TestInstrumentsRegisterNVDBaselineSignals(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	if instruments.NVDBaselineAdvances == nil {
		t.Error("NVDBaselineAdvances = nil, want the advance counter registered")
	}
	if instruments.NVDBaselinePlans == nil {
		t.Error("NVDBaselinePlans = nil, want the plan counter registered")
	}
	if instruments.NVDBaselineTripTransitions == nil {
		t.Error("NVDBaselineTripTransitions = nil, want the trip counter registered")
	}
	if instruments.NVDBaselineDirtyProxyRate == nil {
		t.Error("NVDBaselineDirtyProxyRate = nil, want the dirty-proxy gauge registered")
	}
	if instruments.NVDBaselineOldestDirtyAge == nil {
		t.Error("NVDBaselineOldestDirtyAge = nil, want the oldest-dirty gauge registered")
	}
	ctx := context.Background()
	instruments.RecordNVDBaselineAdvance(ctx, NVDBaselineOutcomeAdvanced)
	instruments.RecordNVDBaselinePlan(ctx, NVDBaselineOutcomePlanned)
	instruments.RecordNVDBaselineTripTransition(ctx, NVDBaselineTripOpened)
	instruments.RecordNVDBaselineDirtySample(ctx, 1, 1)
	names := collectedCounterNames(t, reader)
	for _, name := range []string{
		"eshu_dp_nvd_baseline_advance_total",
		"eshu_dp_nvd_baseline_plans_total",
		"eshu_dp_nvd_baseline_trip_transitions_total",
	} {
		if !names[name] {
			t.Errorf("metric %q not collected, want it registered", name)
		}
	}
}

// TestNVDBaselineOutcomesStayClosed proves the outcome label can never
// carry a scope, window, stamp, or free-form reason: anything outside the
// closed set reads other.
func TestNVDBaselineOutcomesStayClosed(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"advanced", "held", "not_covered", "refused", "planned", "complete"} {
		if got := NVDBaselineOutcome(outcome); got != outcome {
			t.Fatalf("NVDBaselineOutcome(%q) = %q, want identity", outcome, got)
		}
	}
	if got := NVDBaselineOutcome("vuln-intel://nvd/_baseline/run/2002/1"); got != NVDBaselineOutcomeOther {
		t.Fatalf("NVDBaselineOutcome(scope) = %q, want %q", got, NVDBaselineOutcomeOther)
	}
	if got := NVDBaselineOutcome(""); got != NVDBaselineOutcomeOther {
		t.Fatalf("NVDBaselineOutcome(\"\") = %q, want %q", got, NVDBaselineOutcomeOther)
	}
}

// TestRecordNVDBaselineHelpersAreNilSafe proves every record helper skips
// a nil receiver or nil meter: telemetry must never fail a plan, commit,
// or trip sample.
func TestRecordNVDBaselineHelpersAreNilSafe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineAdvance(ctx, "advanced")
	nilInstruments.RecordNVDBaselinePlan(ctx, "planned")
	nilInstruments.RecordNVDBaselineTripTransition(ctx, "opened")
	nilInstruments.RecordNVDBaselineDirtySample(ctx, 0, 0)
}

// TestNVDBaselineSeedRotationAndProgressSignals pins the #10809/#10810
// signals: the seed-rotation counter takes only its closed direction set (a
// typo never mints a label), the progress gauges record values, and nil
// instruments skip every signal.
func TestNVDBaselineSeedRotationAndProgressSignals(t *testing.T) {
	t.Parallel()

	instruments, reader := testBaselineInstruments(t)
	ctx := context.Background()
	instruments.RecordNVDBaselineSeedRotation(ctx, NVDBaselineSeedRotationForward)
	instruments.RecordNVDBaselineSeedRotation(ctx, NVDBaselineSeedRotationRollback)
	instruments.RecordNVDBaselineSeedRotation(ctx, "sideways")
	instruments.RecordNVDBaselineProgress(ctx, 3, 25, 1200, -5)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	directions := map[string]int64{}
	gauges := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name != "eshu_dp_nvd_baseline_seed_rotations_total" {
					continue
				}
				for _, point := range data.DataPoints {
					value, _ := point.Attributes.Value("direction")
					directions[value.AsString()] += point.Value
				}
			case metricdata.Gauge[int64]:
				for _, point := range data.DataPoints {
					gauges[m.Name] = point.Value
				}
			}
		}
	}
	if len(directions) != 2 || directions["forward"] != 1 || directions["rollback"] != 1 {
		t.Fatalf("seed rotation points = %v, want exactly forward=1 and rollback=1 (an unknown direction is dropped)", directions)
	}
	want := map[string]int64{
		"eshu_dp_nvd_baseline_years_complete": 3,
		"eshu_dp_nvd_baseline_years_total":    25,
		"eshu_dp_nvd_baseline_records_done":   1200,
		"eshu_dp_nvd_baseline_records_total":  0,
	}
	for name, value := range want {
		if got, ok := gauges[name]; !ok || got != value {
			t.Fatalf("gauge %s = (%d, %v), want %d", name, got, ok, value)
		}
	}

	var nilInstruments *Instruments
	nilInstruments.RecordNVDBaselineSeedRotation(ctx, NVDBaselineSeedRotationForward)
	nilInstruments.RecordNVDBaselineProgress(ctx, 1, 1, 1, 1)
	(&Instruments{}).RecordNVDBaselineProgress(ctx, 1, 1, 1, 1)
}
