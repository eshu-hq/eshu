// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestOwnGateWakeContract pins the closed site and outcome value sets of
// eshu_dp_reducer_own_gate_wake_total (#7597), its site dimension key, and
// that NewInstruments registers the counter.
func TestOwnGateWakeContract(t *testing.T) {
	t.Parallel()

	if got, want := []string{OwnGateWakeSiteActivation, OwnGateWakeSiteFail}, []string{"activation", "fail"}; !slices.Equal(got, want) {
		t.Fatalf("own-gate wake sites = %v, want the closed set %v", got, want)
	}
	if got, want := []string{OwnGateWakeOutcomeReopened, OwnGateWakeOutcomeNoop, OwnGateWakeOutcomeError},
		[]string{"reopened", "noop", "error"}; !slices.Equal(got, want) {
		t.Fatalf("own-gate wake outcomes = %v, want the closed set %v", got, want)
	}
	if MetricDimensionSite != "site" {
		t.Fatalf("MetricDimensionSite = %q, want %q", MetricDimensionSite, "site")
	}
	if !slices.Contains(MetricDimensionKeys(), MetricDimensionSite) {
		t.Fatalf("MetricDimensionKeys() missing %q", MetricDimensionSite)
	}
	if attr := AttrOwnGateWakeSite(OwnGateWakeSiteFail); string(attr.Key) != "site" || attr.Value.AsString() != "fail" {
		t.Fatalf("AttrOwnGateWakeSite = %v, want site=fail", attr)
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ReducerOwnGateWake == nil {
		t.Fatal("ReducerOwnGateWake counter not registered")
	}
}
