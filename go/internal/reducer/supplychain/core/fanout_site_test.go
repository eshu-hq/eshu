// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fanOutSiteSamples reads the fan-out site histogram: the sample count per
// site and every label key set seen.
func fanOutSiteSamples(t *testing.T, reader *sdkmetric.ManualReader) (map[string]uint64, [][]string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := map[string]uint64{}
	var keySets [][]string
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_site_duration_seconds" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				site, _ := dp.Attributes.Value(attribute.Key("fanout_site"))
				counts[site.AsString()] += dp.Count
				var keys []string
				for _, kv := range dp.Attributes.ToSlice() {
					keys = append(keys, string(kv.Key))
				}
				sort.Strings(keys)
				keySets = append(keySets, keys)
			}
		}
	}
	return counts, keySets
}

// TestFanOutReportsTimeBySite pins the #10972 fan-out site split: a changed
// pass with one holder enters the in-flight read, the holder reads, the
// reopen loop and the state update, and records one sample per site and one
// span attribute per site on the fan-out span, with no id in a label.
func TestFanOutReportsTimeBySite(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	spans := tracetest.NewSpanRecorder()
	loader := &scopedSeedLoader{stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{}}
	handler, port, writer := scopedPassHandler(loader, inst)
	handler.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")
	port.holders = []string{"holder-a"}
	writer.changed = []string{"CVE-2026-9001"}

	intent := fanOutIntent(1, nil)
	intent.Domain = reducercontract.DomainSupplyChainImpact
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	counts, keySets := fanOutSiteSamples(t, reader)
	for _, site := range telemetry.SupplyChainImpactFanOutSites() {
		if counts[site] != 1 {
			t.Fatalf("samples for %s = %d, want 1 (counts %v)", site, counts[site], counts)
		}
	}
	if len(counts) != len(telemetry.SupplyChainImpactFanOutSites()) {
		t.Fatalf("sites = %v, want only the closed set", counts)
	}
	for _, keys := range keySets {
		if want := []string{"domain", "fanout_site"}; !slices.Equal(keys, want) {
			t.Fatalf("label keys = %v, want %v", keys, want)
		}
	}

	var fanOutSpanAttrs map[string]bool
	for _, span := range spans.Ended() {
		if span.Name() != telemetry.SpanSupplyChainImpactFanOut {
			continue
		}
		fanOutSpanAttrs = map[string]bool{}
		for _, kv := range span.Attributes() {
			fanOutSpanAttrs[string(kv.Key)] = true
		}
	}
	for _, key := range []string{"in_flight_early_seconds", "inflight_read_seconds", "holder_reads_seconds", "holder_prefilter_seconds", "reopen_loop_seconds", "state_update_seconds"} {
		if !fanOutSpanAttrs[key] {
			t.Fatalf("fan-out span lacks %s (attrs %v)", key, fanOutSpanAttrs)
		}
	}
}

// TestFanOutSkipsSitesItDoesNotEnter pins that a pass with nothing to fan out
// records no site sample: the sites are time spent, not a heartbeat.
func TestFanOutSkipsSitesItDoesNotEnter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &scopedSeedLoader{stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{}}
	handler, _, _ := scopedPassHandler(loader, inst)
	intent := fanOutIntent(1, nil)
	intent.Domain = reducercontract.DomainSupplyChainImpact
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if counts, _ := fanOutSiteSamples(t, reader); len(counts) != 0 {
		t.Fatalf("site samples = %v, want none for a pass with no changed CVE", counts)
	}
}
