// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"maps"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestOSAdvisoryJoinStatsSeparateLoadedFromJoined pins the #9848 operator
// signal at the builder: an OS advisory fact with no installed row of its
// vendor is counted not_joined, one with a row is counted joined, and a
// registry advisory is not counted at all.
func TestOSAdvisoryJoinStatsSeparateLoadedFromJoined(t *testing.T) {
	t.Parallel()

	debian := convertedOSVFixture(t, "debian-cve-2023-38039.json", false)
	tests := []struct {
		name      string
		envelopes []facts.Envelope
		want      map[osAdvisoryJoinKey]int
	}{
		{"debian advisory with a debian row", append(append([]facts.Envelope{}, debian...), debianCurl("12", "7.88.1-10+deb12u2").envelope()), map[osAdvisoryJoinKey]int{{"debian", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseUntagged}: 1}},
		{"alma advisory with an alma row", append(append([]facts.Envelope{}, convertedOSVFixture(t, "alsa-2021-4511.json", false)...), almaCurl("8", "7.61.1-18.el8").envelope()), map[osAdvisoryJoinKey]int{{"alma", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseUntagged}: 1, {"alma", osAdvisoryJoinOutcomeNotJoined, ""}: 3}},
		{"alma advisory with only a rocky row", append(append([]facts.Envelope{}, convertedOSVFixture(t, "alsa-2021-4511.json", false)...), rockyCurl("8", "7.61.1-18.el8").envelope()), map[osAdvisoryJoinKey]int{{"alma", osAdvisoryJoinOutcomeNotJoined, ""}: 4}},
		{"rocky advisory with a rocky row", append(append([]facts.Envelope{}, convertedOSVFixture(t, "rlsa-2021-1610.json", false)...), rockyCurl("8", "0:7.61.1-16.el8").envelope()), map[osAdvisoryJoinKey]int{{"rocky", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseUntagged}: 1}},
		{"rocky advisory with no row", convertedOSVFixture(t, "rlsa-2021-1610.json", false), map[osAdvisoryJoinKey]int{{"rocky", osAdvisoryJoinOutcomeNotJoined, ""}: 1}},
		{"debian advisory with only an ubuntu row", append(append([]facts.Envelope{}, debian...), ubuntuCurl("22.04", "7.88.1-10+deb12u2").envelope()), map[osAdvisoryJoinKey]int{{"debian", osAdvisoryJoinOutcomeNotJoined, ""}: 1}},
		{"debian advisory with no row", debian, map[osAdvisoryJoinKey]int{{"debian", osAdvisoryJoinOutcomeNotJoined, ""}: 1}},
		{"npm advisory", convertedOSVFixture(t, "ghsa-35jh-r3h4-6jhm.json", false), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(tc.envelopes)
			if err != nil {
				t.Fatalf("build findings: %v", err)
			}
			if !maps.Equal(stats.osAdvisoryJoins, tc.want) {
				t.Fatalf("osAdvisoryJoins = %v, want %v", stats.osAdvisoryJoins, tc.want)
			}
		})
	}
}

// TestEmitOSAdvisoryJoinsCarriesOnlySourceAndOutcome pins the counter's
// closed label set: source (debian, ubuntu, alpine, alma, rocky, other) and outcome
// (joined, not_joined), one point per non-zero cell.
func TestEmitOSAdvisoryJoinsCarriesOnlySourceAndOutcome(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := SupplyChainImpactHandler{Instruments: inst}
	handler.emitOSAdvisoryJoins(context.Background(), map[osAdvisoryJoinKey]int{
		{"debian", osAdvisoryJoinOutcomeNotJoined, ""}:                    3,
		{"alpine", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseMatched}: 2,
		{"ubuntu", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseMatched}: 0,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_os_advisory_joins_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("data point labels = %v, want only source and outcome", point.Attributes)
				}
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				got[source.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"debian/not_joined": 3, "alpine/joined": 2}
	if !maps.Equal(got, want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
}

// TestRecordBuildStatsWritesOSAdvisorySubSignals pins the success-log
// sub-signals: considered and joined are written even when zero, beside the
// existing negated-products count.
func TestRecordBuildStatsWritesOSAdvisorySubSignals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		stats          supplyChainImpactBuildStats
		wantConsidered float64
		wantJoined     float64
	}{
		{"no os advisory", supplyChainImpactBuildStats{}, 0, 0},
		{"loaded none joined", supplyChainImpactBuildStats{osAdvisoryJoins: map[osAdvisoryJoinKey]int{{"debian", osAdvisoryJoinOutcomeNotJoined, ""}: 4}}, 4, 0},
		{"mixed", supplyChainImpactBuildStats{negatedProductsExcluded: 2, osAdvisoryJoins: map[osAdvisoryJoinKey]int{
			{"debian", osAdvisoryJoinOutcomeNotJoined, ""}:                    1,
			{"alpine", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseMatched}: 3,
		}}, 4, 3},
	} {
		subSignals := map[string]float64{}
		SupplyChainImpactHandler{}.recordBuildStats(context.Background(), tc.stats, subSignals)
		if got, ok := subSignals[osAdvisoriesConsideredSubSignal]; !ok || got != tc.wantConsidered {
			t.Errorf("%s: %s = %v (present %v), want %v", tc.name, osAdvisoriesConsideredSubSignal, got, ok, tc.wantConsidered)
		}
		if got, ok := subSignals[osAdvisoriesJoinedSubSignal]; !ok || got != tc.wantJoined {
			t.Errorf("%s: %s = %v (present %v), want %v", tc.name, osAdvisoriesJoinedSubSignal, got, ok, tc.wantJoined)
		}
		if got := subSignals[negatedProductsExcludedSubSignal]; got != float64(tc.stats.negatedProductsExcluded) {
			t.Errorf("%s: %s = %v, want %d", tc.name, negatedProductsExcludedSubSignal, got, tc.stats.negatedProductsExcluded)
		}
	}
}
