// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNVDWindowCodeMapsOntoClosedSet(t *testing.T) {
	t.Parallel()
	for _, code := range []string{NVDWindowEndAfter, NVDWindowExceedsMaxRange, NVDWindowOlderThanMinimum} {
		if got := NVDWindowCode(code); got != code {
			t.Fatalf("NVDWindowCode(%q) = %q, want itself", code, got)
		}
	}
	if got := NVDWindowCode("window_from_nowhere"); got != NVDWindowOther {
		t.Fatalf("NVDWindowCode(unknown) = %q, want %q", got, NVDWindowOther)
	}
	if got := len(NVDWindowCodes()); got != 4 {
		t.Fatalf("len(NVDWindowCodes()) = %d, want the three ruled codes plus other", got)
	}
}

func nvdCursorTestMicros(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed.UTC().UnixMicro()
}

func TestNVDCursorLagTrackerHoldsMaxLagPerSource(t *testing.T) {
	t.Parallel()
	var nilTracker *NVDCursorLagTracker
	nilTracker.RecordCursorPosition("nvd", "scope-a", 1, 2)
	if got := nilTracker.LagSecondsBySource(); len(got) != 0 {
		t.Fatalf("nil LagSecondsBySource() = %v, want empty", got)
	}
	tracker := NewNVDCursorLagTracker()
	tracker.RecordCursorPosition("nvd", "", 1, 2)
	if got := tracker.LagSecondsBySource(); len(got) != 0 {
		t.Fatalf("blank key recorded lag %v, want nothing", got)
	}
	micros := func(value string) int64 { return nvdCursorTestMicros(t, value) }
	tracker.RecordCursorPosition("nvd", "scope-a",
		micros("2026-01-03T00:00:00Z"), micros("2026-01-05T00:00:00Z"))
	tracker.RecordCursorPosition("nvd", "scope-b",
		micros("2026-01-03T00:00:00Z"), micros("2026-01-04T00:00:00Z"))
	tracker.RecordCursorPosition("nvd", "scope-behind",
		micros("2026-01-05T00:00:00Z"), micros("2026-01-03T00:00:00Z"))
	lags := tracker.LagSecondsBySource()
	if got := lags["nvd"]; got != 172800 {
		t.Fatalf("lag = %v seconds, want the 2-day max (172800), floored negatives ignored", got)
	}
}

func TestNVDCursorLagTrackerKeepsMaxPerKey(t *testing.T) {
	t.Parallel()
	tracker := NewNVDCursorLagTracker()
	// The late record carries a nonzero lag (cursor Jan 5, head Jan 7), so
	// a blind store that lets the stale record win reads 0 while max-keep
	// holds 172800: this cell fails on the pre-fix implementation.
	tracker.RecordCursorPosition("nvd", "scope-a",
		nvdCursorTestMicros(t, "2026-01-05T00:00:00Z"), nvdCursorTestMicros(t, "2026-01-07T00:00:00Z"))
	tracker.RecordCursorPosition("nvd", "scope-a",
		nvdCursorTestMicros(t, "2026-01-03T00:00:00Z"), nvdCursorTestMicros(t, "2026-01-03T00:00:00Z"))
	if got := tracker.LagSecondsBySource()["nvd"]; got != 172800 {
		t.Fatalf("lag after stale record = %v, want 172800 (late-then-stale holds)", got)
	}
}

func TestNVDCursorLagTrackerIsRaceFree(t *testing.T) {
	t.Parallel()
	tracker := NewNVDCursorLagTracker()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("scope-%d", worker%4)
			for i := 0; i < 200; i++ {
				tracker.RecordCursorPosition("nvd", key, int64(i), int64(i+10))
				_ = tracker.LagSecondsBySource()
			}
		}()
	}
	wg.Wait()
}

func TestInstrumentsReportNVDCursorLagGauge(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst, err := NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	inst.NVDCursorLags.RecordCursorPosition("nvd", "scope-a", 1_000_000, 172_801_000_000)
	inst.NVDTimeAnomalies.Add(context.Background(), 1, metric.WithAttributes(attribute.String(MetricDimensionSource, "nvd")))

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var gaugeFound, counterFound bool
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			switch m.Name {
			case "eshu_dp_vulnerability_nvd_cursor_lag_seconds":
				gauge, ok := m.Data.(metricdata.Gauge[float64])
				if !ok {
					t.Fatalf("%s data = %T, want Gauge[float64]", m.Name, m.Data)
				}
				if len(gauge.DataPoints) != 1 {
					t.Fatalf("data points = %d, want 1 (max per source)", len(gauge.DataPoints))
				}
				point := gauge.DataPoints[0]
				if point.Value != 172800 {
					t.Fatalf("lag = %v, want 172800 seconds from server-stamped operands only", point.Value)
				}
				if got, ok := point.Attributes.Value("source"); !ok || got.AsString() != "nvd" {
					t.Fatalf("source = %v, %v; want nvd and no other label", got, ok)
				}
				gaugeFound = true
			case "eshu_dp_vulnerability_nvd_time_anomalies_total":
				counterFound = true
			}
		}
	}
	if !gaugeFound {
		t.Fatal("eshu_dp_vulnerability_nvd_cursor_lag_seconds was not collected")
	}
	if !counterFound {
		t.Fatal("eshu_dp_vulnerability_nvd_time_anomalies_total was not collected")
	}
}
