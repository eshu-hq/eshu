// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// routeCounts reads eshu_dp_supply_chain_impact_read_route_total by route and
// asserts every datapoint carries the route label and nothing else (a bounded,
// five-value label: no filter value, no caller identity).
func routeCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_supply_chain_impact_read_route_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data = %T, want Sum[int64]", record.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 1 {
					t.Fatalf("datapoint attributes = %d, want 1 (route)", point.Attributes.Len())
				}
				route, _ := point.Attributes.Value("route")
				out[route.AsString()] += point.Value
			}
		}
	}
	return out
}

// routeDurationCounts reads the sample count of
// eshu_dp_supply_chain_impact_read_duration_seconds by route and asserts each
// datapoint carries only the route label.
func routeDurationCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]uint64{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_supply_chain_impact_read_duration_seconds" {
				continue
			}
			histogram, ok := record.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("histogram data = %T, want Histogram[float64]", record.Data)
			}
			for _, point := range histogram.DataPoints {
				if point.Attributes.Len() != 1 {
					t.Fatalf("datapoint attributes = %d, want 1 (route)", point.Attributes.Len())
				}
				route, _ := point.Attributes.Value("route")
				out[route.AsString()] += point.Count
			}
		}
	}
	return out
}

func routeTelemetryHarness(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader, *tracetest.SpanRecorder, context.Context, func()) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("impact-read-route-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("impact-read-route-test")
	ctx, span := tracer.Start(context.Background(), "impact-read")
	return instruments, reader, recorder, ctx, func() { span.End() }
}

// routeSpanAttribute returns the read-route attribute of the one recorded span.
func routeSpanAttribute(t *testing.T, recorder *tracetest.SpanRecorder) string {
	t.Helper()
	for _, span := range recorder.Ended() {
		for _, attribute := range span.Attributes() {
			if string(attribute.Key) == RouteSpanAttribute {
				return attribute.Value.AsString()
			}
		}
	}
	return ""
}

// TestEveryReadCountsItsRoute pins ruling V proof 9: each list, count, inventory
// and grouped call increments the per-route counter exactly once with the route
// its statement reads, and records the same route on the active span. The gate
// off path counts "legacy"; a refused (not-ready) call still counts the route it
// tried, so an operator sees which route 503s.
func TestEveryReadCountsItsRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		for what, call := range map[string]func(ctx context.Context, instruments *telemetry.Instruments) error{
			"list": func(ctx context.Context, instruments *telemetry.Instruments) error {
				store := NewPostgresFindingStoreWithReadStore(routeQueryer(), true)
				store.Instruments = instruments
				filter := FindingFilter{Limit: 10, Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedScopeIDs = []string{"scope:a"}
				}
				_, err := store.ListSupplyChainImpactFindings(ctx, filter)
				return err
			},
			"count": func(ctx context.Context, instruments *telemetry.Instruments) error {
				store := NewPostgresAggregateStoreWithReadStore(routeQueryer(), true)
				store.Instruments = instruments
				filter := AggregateFilter{Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedRepositoryIDs = []string{"repository:r"}
				}
				_, err := store.CountSupplyChainImpactFindings(ctx, filter)
				return err
			},
			"inventory": func(ctx context.Context, instruments *telemetry.Instruments) error {
				store := NewPostgresAggregateStoreWithReadStore(routeQueryer(), true)
				store.Instruments = instruments
				filter := AggregateFilter{Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedRepositoryIDs = []string{"repository:r"}
				}
				_, err := store.SupplyChainImpactInventory(ctx, filter, InventoryByStatus, 10, 0)
				return err
			},
			"grouped": func(ctx context.Context, instruments *telemetry.Instruments) error {
				store := NewPostgresGroupedInventoryStoreWithReadStore(routeQueryer(), true, nil)
				store.Instruments = instruments
				store.Now = func() time.Time { return time.Now().UTC() }
				filter := AggregateFilter{ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedScopeIDs = []string{"scope:a"}
				}
				_, err := store.SupplyChainImpactGroupedInventory(ctx, filter, GroupedInventoryByService, 10, 0)
				return err
			},
		} {
			t.Run(tc.name+"/"+what, func(t *testing.T) {
				t.Parallel()
				instruments, reader, recorder, ctx, end := routeTelemetryHarness(t)
				if err := call(ctx, instruments); err != nil {
					t.Fatalf("%s = %v", what, err)
				}
				end()
				counts := routeCounts(t, reader)
				if len(counts) != 1 || counts[string(tc.want)] != 1 {
					t.Fatalf("route counter = %v, want exactly one count for %q", counts, tc.want)
				}
				if durations := routeDurationCounts(t, reader); len(durations) != 1 || durations[string(tc.want)] != 1 {
					t.Fatalf("read-duration samples = %v, want exactly one sample for %q", durations, tc.want)
				}
				if got := routeSpanAttribute(t, recorder); got != string(tc.want) {
					t.Fatalf("span route attribute = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// TestGateOffCountsTheLegacyRoute pins the counter for the reversible path.
func TestGateOffCountsTheLegacyRoute(t *testing.T) {
	t.Parallel()
	instruments, reader, recorder, ctx, end := routeTelemetryHarness(t)
	store := NewPostgresFindingStoreWithReadStore(routeQueryer(), false)
	store.Instruments = instruments
	if _, err := store.ListSupplyChainImpactFindings(ctx, FindingFilter{Limit: 10, Status: "affected_exact", DetectionProfile: ProfilePrecise}); err != nil {
		t.Fatalf("list = %v", err)
	}
	end()
	if counts := routeCounts(t, reader); len(counts) != 1 || counts[string(RouteLegacy)] != 1 {
		t.Fatalf("route counter = %v, want one legacy count", counts)
	}
	if got := routeSpanAttribute(t, recorder); got != string(RouteLegacy) {
		t.Fatalf("span route attribute = %q, want legacy", got)
	}
}

// TestNilInstrumentsDoNotPanic keeps the nil-tolerant seam the other query
// stores follow: a store built without the telemetry stack still answers.
func TestNilInstrumentsDoNotPanic(t *testing.T) {
	t.Parallel()
	store := NewPostgresFindingStoreWithReadStore(routeQueryer(), true)
	if _, err := store.ListSupplyChainImpactFindings(context.Background(), FindingFilter{Limit: 10, Status: "affected_exact"}); err != nil {
		t.Fatalf("list = %v", err)
	}
}

// TestExplainCountsEachStatementByRoute pins ruling decision 10: explain
// statements are counted and timed by route, and the reads that stay on the fact
// table (the ungranted selector without an image, the identity fallback of a
// public-id miss, and everything with the gate off) show up as legacy, so the
// metric reports how much explain traffic still takes the legacy read.
func TestExplainCountsEachStatementByRoute(t *testing.T) {
	t.Parallel()
	granted := []string{"scope:a"}
	for name, tc := range map[string]struct {
		gate   bool
		filter packet.ExplanationFilter
		want   map[string]int64
	}{
		"gate off public id miss": {false, packet.ExplanationFilter{FindingID: "finding:a"}, map[string]int64{"legacy": 2}},
		"ungranted public id miss then identity fallback": {
			true, packet.ExplanationFilter{FindingID: "finding:a"}, map[string]int64{"supports_finding_id": 1, "legacy": 1},
		},
		"ungranted selector": {
			true, packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p"}, map[string]int64{"legacy": 1},
		},
		"ungranted image selector": {
			true, packet.ExplanationFilter{CVEID: "CVE-2026-1", ImageRef: "registry.example/a:1"}, map[string]int64{"supports_image": 1},
		},
		"granted selector": {
			true, packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p", AllowedScopeIDs: granted}, map[string]int64{"supports_grant": 1},
		},
		"granted public id miss": {
			true, packet.ExplanationFilter{FindingID: "finding:a", AllowedScopeIDs: granted}, map[string]int64{"supports_grant": 2},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instruments, reader, recorder, ctx, end := routeTelemetryHarness(t)
			store := NewPostgresFindingStoreWithReadStore(&explainRouteQueryer{ready: true}, tc.gate)
			store.Instruments = instruments
			if _, err := store.ExplainSupplyChainImpact(ctx, tc.filter); !errors.Is(err, ErrExplanationNotFound) {
				t.Fatalf("explain = %v, want ErrExplanationNotFound for an empty store", err)
			}
			end()
			counts := routeCounts(t, reader)
			if len(counts) != len(tc.want) {
				t.Fatalf("route counter = %v, want %v", counts, tc.want)
			}
			for route, n := range tc.want {
				if counts[route] != n {
					t.Fatalf("route counter = %v, want %v", counts, tc.want)
				}
				if got := routeDurationCounts(t, reader)[route]; got != uint64(n) {
					t.Fatalf("read-duration samples for %q = %d, want %d", route, got, n)
				}
			}
			if routeSpanAttribute(t, recorder) == "" {
				t.Fatal("the explain recorded no route attribute on its span")
			}
		})
	}
}
