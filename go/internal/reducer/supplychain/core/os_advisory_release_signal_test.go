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

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestOSAdvisoryReleaseResult pins the closed result set of the release
// selection signal (#9806 PR-2b): what the join can say about the installed
// row's release line for one joined OS advisory fact.
func TestOSAdvisoryReleaseResult(t *testing.T) {
	t.Parallel()

	bookworm := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}
	untagged := osvDebianCurlFact()
	for i := range untagged.AffectedRanges {
		untagged.AffectedRanges[i].Releases = nil
	}
	vendor := osvDebianCurlFact()
	vendor.Source, vendor.AdvisoryID = "debian", "DSA-0000-1"
	for _, tc := range []struct {
		name string
		pkg  supplychainmodel.AffectedPackage
		row  supplychainmodel.OSPackage
		want string
	}{
		{"host line listed", osvDebianCurlFact(), bookworm, osAdvisoryReleaseMatched},
		{"host line unlisted", osvDebianCurlFact(), supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "11"}, osAdvisoryReleaseUnlisted},
		{"untagged fact", untagged, bookworm, osAdvisoryReleaseUntagged},
		{"host release unknown", osvDebianCurlFact(), supplychainmodel.OSPackage{Distro: "debian"}, osAdvisoryReleaseHostUnknown},
		{"vendor-sourced fact", vendor, bookworm, osAdvisoryReleaseVendorSource},
	} {
		if got := osAdvisoryReleaseResult(tc.pkg, tc.row); got != tc.want {
			t.Errorf("%s: osAdvisoryReleaseResult = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestEmitOSAdvisoryReleaseSelections pins the new counter: one point per
// non-zero joined (source, result) cell, labeled only source and result; the
// join counter keeps summing over results with only source and outcome.
func TestEmitOSAdvisoryReleaseSelections(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	subSignals := map[string]float64{}
	SupplyChainImpactHandler{Instruments: inst}.recordBuildStats(context.Background(), supplyChainImpactBuildStats{
		osAdvisoryJoins: map[osAdvisoryJoinKey]int{
			{"debian", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseMatched}:  2,
			{"debian", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseUnlisted}: 1,
			{"alpine", osAdvisoryJoinOutcomeJoined, osAdvisoryReleaseUntagged}: 0,
			{"ubuntu", osAdvisoryJoinOutcomeNotJoined, ""}:                     4,
		},
	}, subSignals)

	// The success log carries the same split, so an operator without a
	// metrics backend sees tagged, untagged, and unlisted counts too.
	for name, want := range map[string]float64{
		osAdvisoriesReleaseMatchedSubSignal:  2,
		osAdvisoriesReleaseUnlistedSubSignal: 1,
		osAdvisoriesReleaseUntaggedSubSignal: 0,
	} {
		if got, ok := subSignals[name]; !ok || got != want {
			t.Errorf("%s = %v (present %v), want %v", name, got, ok, want)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	selections := map[string]int64{}
	joins := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				switch m.Name {
				case "eshu_dp_supply_chain_impact_os_advisory_release_selections_total":
					if point.Attributes.Len() != 2 {
						t.Fatalf("selection labels = %v, want only source and result", point.Attributes)
					}
					result, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionResult))
					selections[source.AsString()+"/"+result.AsString()] += point.Value
				case "eshu_dp_supply_chain_impact_os_advisory_joins_total":
					outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
					joins[source.AsString()+"/"+outcome.AsString()] += point.Value
				}
			}
		}
	}
	if want := map[string]int64{"debian/release_matched": 2, "debian/release_unlisted": 1}; !maps.Equal(selections, want) {
		t.Fatalf("selection points = %v, want %v", selections, want)
	}
	if want := map[string]int64{"debian/joined": 3, "ubuntu/not_joined": 4}; !maps.Equal(joins, want) {
		t.Fatalf("join points = %v, want %v", joins, want)
	}
}
