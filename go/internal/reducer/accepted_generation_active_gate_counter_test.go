// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// gateDecisionsCounterValue reads the cumulative value for
// eshu_dp_repo_dependency_gate_decisions_total at the given attribute set.
func gateDecisionsCounterValue(t *testing.T, rm metricdata.ResourceMetrics, wantAttrs map[string]string) int64 {
	t.Helper()
	const metricName = "eshu_dp_repo_dependency_gate_decisions_total"
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", metricName, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attrsMatchGateDecision(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", metricName, wantAttrs)
	return 0
}

func attrsMatchGateDecision(attrs []attribute.KeyValue, wantAttrs map[string]string) bool {
	for wantKey, wantVal := range wantAttrs {
		found := false
		for _, a := range attrs {
			if string(a.Key) == wantKey && a.Value.AsString() == wantVal {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestGateAcceptedGenerationOnActiveCountersBypassed proves the counter
// increments with decision="bypassed" for scope-gen source runs that do not
// require a relationship generation activation gate (code-import,
// package-consumption).
func TestGateAcceptedGenerationOnActiveCountersBypassed(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("scope-gen-abc", true),
		func(string) (bool, error) { return false, nil },
		instruments,
	)

	key := SharedProjectionAcceptanceKey{
		ScopeID:          "git-repository-scope:repository:r_app",
		AcceptanceUnitID: "repository:r_app",
		SourceRunID:      "code_import_repo_dependency:git-repository-scope:repository:r_app",
	}
	gen, ok := gated(key)
	if !ok || gen != "scope-gen-abc" {
		t.Fatalf("gated lookup = (%q, %v), want (scope-gen-abc, true)", gen, ok)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "bypassed",
	}); got != 1 {
		t.Fatalf("repo_dependency_gate_decisions_total[bypassed] = %d, want 1", got)
	}
}

// TestGateAcceptedGenerationOnActiveCountersDeferredInactive proves the
// counter increments with decision="deferred_inactive" when the relationship
// generation exists but is not yet active.
func TestGateAcceptedGenerationOnActiveCountersDeferredInactive(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return false, nil }, // inactive
		instruments,
	)

	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	if gen, ok := gated(key); ok {
		t.Fatalf("gated lookup = (%q, true), want deferred for inactive gen", gen)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "deferred_inactive",
	}); got != 1 {
		t.Fatalf("repo_dependency_gate_decisions_total[deferred_inactive] = %d, want 1", got)
	}
}

// TestGateAcceptedGenerationOnActiveCountersDeferredError proves the counter
// increments with decision="deferred_error" when IsGenerationActive errors.
func TestGateAcceptedGenerationOnActiveCountersDeferredError(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return false, errors.New("transient error") },
		instruments,
	)

	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	if gen, ok := gated(key); ok {
		t.Fatalf("gated lookup = (%q, true), want deferred on error", gen)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "deferred_error",
	}); got != 1 {
		t.Fatalf("repo_dependency_gate_decisions_total[deferred_error] = %d, want 1", got)
	}
}

// TestGateAcceptedGenerationOnActiveCountersPerKeyNotPerPrefetchBatch proves
// the counter increments once per key resolved, not per prefetch batch.
// Three keys in one batch produce three counter increments.
func TestGateAcceptedGenerationOnActiveCountersPerKeyNotPerPrefetchBatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	// Simulate bypass for multiple keys — use code-import source runs.
	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("scope-gen-abc", true),
		func(string) (bool, error) { return false, nil },
		instruments,
	)

	keys := []SharedProjectionAcceptanceKey{
		{ScopeID: "s1", AcceptanceUnitID: "r1", SourceRunID: "code_import_repo_dependency:s1"},
		{ScopeID: "s2", AcceptanceUnitID: "r2", SourceRunID: "code_import_repo_dependency:s2"},
		{ScopeID: "s3", AcceptanceUnitID: "r3", SourceRunID: "code_import_repo_dependency:s3"},
	}
	for _, key := range keys {
		if _, ok := gated(key); !ok {
			t.Fatalf("key %v should pass through", key)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "bypassed",
	}); got != 3 {
		t.Fatalf("repo_dependency_gate_decisions_total[bypassed] = %d, want 3 (one per key)", got)
	}
}

// TestGateAcceptedGenerationOnActiveNilInstrumentsNoPanic proves the gate
// tolerates a nil *Instruments handle (graceful fallback).
func TestGateAcceptedGenerationOnActiveNilInstrumentsNoPanic(t *testing.T) {
	t.Parallel()

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return false, nil },
		nil,
	)

	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	// Must not panic — no instruments. Defer as expected.
	if gen, ok := gated(key); ok {
		t.Fatalf("gated lookup = (%q, true), want deferred for inactive gen (nil instruments)", gen)
	}
}

// TestGateAcceptedGenerationOnActiveCountersRepeatedCalls proves
// the counter increments exactly once per lookup call, not once per generation.
// Calling the same gate 3 times on an inactive gen increments the counter 3 times.
func TestGateAcceptedGenerationOnActiveCountersRepeatedCalls(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return false, nil },
		instruments,
	)

	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	for i := 0; i < 3; i++ {
		if gen, ok := gated(key); ok {
			t.Fatalf("gated lookup %d = (%q, true), want deferred", i, gen)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "deferred_inactive",
	}); got != 3 {
		t.Fatalf("repo_dependency_gate_decisions_total[deferred_inactive] = %d, want 3 (one per call)", got)
	}
}

// TestGateAcceptedGenerationOnActiveCountersNoIncrementForMissingAcceptance proves
// that missing acceptance rows (base lookup returns false) do NOT increment the
// counter — the gate is never exercised and no decision is made.
func TestGateAcceptedGenerationOnActiveCountersNoIncrementForMissingAcceptance(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("", false),
		func(string) (bool, error) { return false, nil },
		instruments,
	)

	key := SharedProjectionAcceptanceKey{AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	if _, ok := gated(key); ok {
		t.Fatal("gated lookup = true, want false for missing acceptance")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	// No data points should exist for any decision label.
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_repo_dependency_gate_decisions_total" {
				t.Fatal("repo_dependency_gate_decisions_total data points found when gate was never exercised (missing acceptance row)")
			}
		}
	}
}

// TestGateAcceptedGenerationOnActiveCountersActive proves the counter
// increments with decision="active" when the generation is accepted,
// the source run requires the gate, and the generation IS active.
func TestGateAcceptedGenerationOnActiveCountersActive(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return true, nil }, // active
		instruments,
	)

	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}
	gen, ok := gated(key)
	if !ok || gen != "gen-2" {
		t.Fatalf("gated lookup = (%q, %v), want (gen-2, true)", gen, ok)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := gateDecisionsCounterValue(t, rm, map[string]string{
		telemetry.MetricDimensionDecision: "active",
	}); got != 1 {
		t.Fatalf("repo_dependency_gate_decisions_total[active] = %d, want 1", got)
	}
}
