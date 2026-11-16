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
