// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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
