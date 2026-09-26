// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scopedWithheldCounts collects eshu_dp_query_impact_scoped_paths_withheld_total
// by reason for one route, asserting each datapoint carries only route and
// reason.
func scopedWithheldCounts(t *testing.T, reader *sdkmetric.ManualReader, route string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_query_impact_scoped_paths_withheld_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data = %T, want Sum[int64]", record.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("datapoint attributes = %d, want 2 (route, reason)", point.Attributes.Len())
				}
				gotRoute, _ := point.Attributes.Value("route")
				if gotRoute.AsString() != route {
					continue
				}
				reason, _ := point.Attributes.Value("reason")
				out[reason.AsString()] += point.Value
			}
		}
	}
	return out
}

// B1 (#5167 review): an exposure path ending on a sink class no grant can own
// (CidrBlock here) is counted as withheld_sink_class, not ungranted_node; the
// foreign interior, the repo-b-written shared uid, and the foreign
// CloudResource sink stay ungranted_node.
func TestScopedTraceExposurePathCountsWithheldSinkClass(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("impact-scoped-withheld-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	a := tenantAAuth()
	h := exposureHandler(&twoTenantGraph{exposureRows: exposureFixtureRows()})
	h.Instruments = instruments
	postImpact(t, h, exposureRoute, `{"source_entity_id":"fn-a"}`, &a)

	got := scopedWithheldCounts(t, reader, ownership.RouteTraceExposurePath)
	if got[ownership.ReasonWithheldSinkClass] != 1 {
		t.Errorf("withheld_sink_class = %d, want 1 (the CidrBlock sink); counts %v", got[ownership.ReasonWithheldSinkClass], got)
	}
	if got[ownership.ReasonUngrantedNode] != 3 {
		t.Errorf("ungranted_node = %d, want 3; counts %v", got[ownership.ReasonUngrantedNode], got)
	}
}
