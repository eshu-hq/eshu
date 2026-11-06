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

func upstreamGroupCells(t *testing.T, envelopes []facts.Envelope) (map[string]int64, map[string]float64, supplyChainImpactBuildStats) {
	t.Helper()

	_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(envelopes)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactFindingsAndExclusions() error = %v", err)
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	subSignals := map[string]float64{}
	SupplyChainImpactHandler{Instruments: inst}.recordBuildStats(context.Background(), stats, subSignals)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	cells := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_supply_chain_impact_os_advisory_upstream_groups_total" {
				continue
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("upstream group labels = %v, want only source and result", point.Attributes)
				}
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				result, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionResult))
				cells[source.AsString()+"/"+result.AsString()] += point.Value
			}
		}
	}
	return cells, subSignals, stats
}

// TestOSAdvisoryUpstreamGroupCounter pins the #9914 signal: one point per
// examined vulnerability.cve record, labeled only by the closed source set and
// the grouped/ungrouped result, with the same split on the success log. A
// record that is not an OS vendor record and has no upstream list is not
// examined, so a plain NVD fleet adds no series.
func TestOSAdvisoryUpstreamGroupCounter(t *testing.T) {
	t.Parallel()

	withUpstream := func(key string) []facts.Envelope {
		return convertedOSVFixture(t, key, false)
	}
	var envelopes []facts.Envelope
	envelopes = append(envelopes, withUpstream("built-signal-dsa")...)
	envelopes = append(envelopes, withUpstream("built-signal-dla")...)
	envelopes = append(envelopes, withUpstream("built-signal-debian-cve")...)
	envelopes = append(envelopes, withUpstream("built-signal-usn")...)
	envelopes = append(envelopes, withUpstream("built-signal-alpine")...)
	envelopes = append(envelopes, withUpstream("built-signal-some-other")...)
	envelopes = append(envelopes, nvdCVEFact("CVE-2024-0001"), nvdCVEFact("CVE-2024-0006"))

	cells, subSignals, _ := upstreamGroupCells(t, envelopes)
	want := map[string]int64{
		"debian/grouped":   2, // DSA-9998-1, DLA-9999-1
		"debian/ungrouped": 1, // DEBIAN-CVE-2024-0002 names only itself
		"ubuntu/grouped":   1,
		"alpine/ungrouped": 1, // no upstream list reached the reducer
		"other/grouped":    1,
	}
	if !maps.Equal(cells, want) {
		t.Fatalf("upstream group points = %v, want %v", cells, want)
	}
	if got := subSignals[osAdvisoriesGroupedSubSignal]; got != 4 {
		t.Errorf("%s = %v, want 4", osAdvisoriesGroupedSubSignal, got)
	}
	if got := subSignals[osAdvisoriesUngroupedSubSignal]; got != 2 {
		t.Errorf("%s = %v, want 2", osAdvisoriesUngroupedSubSignal, got)
	}
}

// TestOSAdvisoryUpstreamGroupCounterPlantedRed is the planted RED for the
// signal: the same fleet converted from records that carry no upstream list
// (the shape stored before the producer wrote one) reads zero grouped.
func TestOSAdvisoryUpstreamGroupCounterPlantedRed(t *testing.T) {
	t.Parallel()

	var envelopes []facts.Envelope
	envelopes = append(envelopes, convertedOSVFixture(t, "built-signal-dsa-noupstream", false)...)
	envelopes = append(envelopes, convertedOSVFixture(t, "built-signal-alpine-noupstream", false)...)

	cells, subSignals, _ := upstreamGroupCells(t, envelopes)
	if want := map[string]int64{"debian/ungrouped": 1, "alpine/ungrouped": 1}; !maps.Equal(cells, want) {
		t.Fatalf("upstream group points = %v, want %v", cells, want)
	}
	if subSignals[osAdvisoriesGroupedSubSignal] != 0 || subSignals[osAdvisoriesUngroupedSubSignal] != 2 {
		t.Fatalf("sub-signals grouped/ungrouped = %v/%v, want 0/2", subSignals[osAdvisoriesGroupedSubSignal], subSignals[osAdvisoriesUngroupedSubSignal])
	}
}

// TestOSAdvisoryJoinCensusCountsEachFactOnce: a bulletin filed under several
// CVEs is still one loaded advisory fact, so the considered and joined counts
// of the join counter do not multiply by the bundle size.
func TestOSAdvisoryJoinCensusCountsEachFactOnce(t *testing.T) {
	t.Parallel()

	envelopes := append(convertedOSVFixture(t, "built-census-usn", false), ubuntuCurl("22.04", "7.81.0-1ubuntu1.1").envelope())
	_, _, stats := upstreamGroupCells(t, envelopes)
	if got := stats.osAdvisoriesConsidered(); got != 1 {
		t.Errorf("osAdvisoriesConsidered = %d, want the bundle's 1 affected fact", got)
	}
	if got := stats.osAdvisoriesJoined(); got != 1 {
		t.Errorf("osAdvisoriesJoined = %d, want 1", got)
	}
}
