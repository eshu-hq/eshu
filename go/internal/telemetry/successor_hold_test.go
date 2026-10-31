// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSuccessorHoldTrackerCountsOnlyRepeatedRefusals(t *testing.T) {
	t.Parallel()

	tracker := NewSuccessorHoldTracker()
	for i := 1; i < SuccessorHoldThreshold; i++ {
		tracker.RecordRefusal("scope-a", "vulnerability_intelligence")
	}
	if got := tracker.HeldByKind(); len(got) != 0 {
		t.Fatalf("HeldByKind() = %v after %d refusals, want empty below the threshold", got, SuccessorHoldThreshold-1)
	}
	if got := tracker.RecordRefusal("scope-a", "vulnerability_intelligence"); got != SuccessorHoldThreshold {
		t.Fatalf("RecordRefusal() = %d, want %d", got, SuccessorHoldThreshold)
	}
	held := tracker.HeldByKind()
	if got, want := held["vulnerability_intelligence"], int64(1); got != want {
		t.Fatalf("held[vulnerability_intelligence] = %d, want %d", got, want)
	}
}

func TestSuccessorHoldTrackerClearReleasesTheScope(t *testing.T) {
	t.Parallel()

	tracker := NewSuccessorHoldTracker()
	for i := 0; i < SuccessorHoldThreshold; i++ {
		tracker.RecordRefusal("scope-a", "vulnerability_intelligence")
		tracker.RecordRefusal("scope-b", "vulnerability_intelligence")
	}
	tracker.Clear("scope-a")
	if got, want := tracker.HeldByKind()["vulnerability_intelligence"], int64(1); got != want {
		t.Fatalf("held after clearing scope-a = %d, want %d", got, want)
	}
	if got := tracker.RecordRefusal("scope-a", "vulnerability_intelligence"); got != 1 {
		t.Fatalf("RecordRefusal() after Clear = %d, want 1 (a stored commit resets the run)", got)
	}
	tracker.Clear("scope-unknown")
}

func TestSuccessorHoldTrackerNilAndBlankAreSafe(t *testing.T) {
	t.Parallel()

	var nilTracker *SuccessorHoldTracker
	if got := nilTracker.RecordRefusal("scope-a", "k"); got != 0 {
		t.Fatalf("nil RecordRefusal() = %d, want 0", got)
	}
	nilTracker.Clear("scope-a")
	if got := nilTracker.HeldByKind(); len(got) != 0 {
		t.Fatalf("nil HeldByKind() = %v, want empty", got)
	}
	tracker := NewSuccessorHoldTracker()
	if got := tracker.RecordRefusal("  ", "k"); got != 0 {
		t.Fatalf("blank-scope RecordRefusal() = %d, want 0", got)
	}
}

func TestSuccessorHoldTrackerIsRaceFree(t *testing.T) {
	t.Parallel()

	tracker := NewSuccessorHoldTracker()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scopeID := fmt.Sprintf("scope-%d", worker%4)
			for i := 0; i < 200; i++ {
				tracker.RecordRefusal(scopeID, "vulnerability_intelligence")
				_ = tracker.HeldByKind()
				if i%50 == 49 {
					tracker.Clear(scopeID)
				}
			}
		}()
	}
	wg.Wait()
}

func TestInstrumentsReportHeldStaleScopesGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst, err := NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	for i := 0; i < SuccessorHoldThreshold; i++ {
		inst.SuccessorHolds.RecordRefusal("scope-a", "vulnerability_intelligence")
	}
	inst.SuccessorHolds.RecordRefusal("scope-b", "vulnerability_intelligence")

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var found bool
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_ingestion_successor_held_stale_scopes" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Gauge[int64]", m.Name, m.Data)
			}
			if len(gauge.DataPoints) != 1 {
				t.Fatalf("data points = %d, want 1 (scope-b is below the threshold)", len(gauge.DataPoints))
			}
			point := gauge.DataPoints[0]
			if point.Value != 1 {
				t.Fatalf("held scopes = %d, want 1", point.Value)
			}
			if got, ok := point.Attributes.Value("collector_kind"); !ok || got.AsString() != "vulnerability_intelligence" {
				t.Fatalf("collector_kind = %v, %v; want vulnerability_intelligence", got, ok)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("eshu_dp_ingestion_successor_held_stale_scopes was not collected")
	}
}

// TestSuccessorHoldThresholdIsThree pins the number the runbook and the metric
// reference state. Every other test uses the constant symbolically, so a change
// to it would otherwise pass them all while the docs went wrong (review G7).
func TestSuccessorHoldThresholdIsThree(t *testing.T) {
	t.Parallel()
	if SuccessorHoldThreshold != 3 {
		t.Fatalf("SuccessorHoldThreshold = %d; the runbook and metric reference say three consecutive refusals, change them together", SuccessorHoldThreshold)
	}
}
