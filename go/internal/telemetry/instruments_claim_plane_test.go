// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestClaimPlaneInstrumentsRegisterAndEmit pins the #8875 claim-plane client
// and claim-store selection instruments: NewInstruments registers all three,
// the duration histogram carries second-scale boundaries ending at the 10 s
// client timeout, and the labels use only the existing operation, outcome,
// and backend_kind dimensions.
func TestClaimPlaneInstrumentsRegisterAndEmit(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.WorkflowClaimPlaneRequests == nil ||
		instruments.WorkflowClaimPlaneRequestDuration == nil ||
		instruments.WorkflowControlStoreSelections == nil {
		t.Fatal("claim-plane instruments not registered")
	}
	ctx := context.Background()
	requestAttrs := metric.WithAttributes(AttrOperation("HeartbeatClaim"), AttrOutcome("ok"))
	instruments.WorkflowClaimPlaneRequests.Add(ctx, 1, requestAttrs)
	instruments.WorkflowClaimPlaneRequestDuration.Record(ctx, 0.02, requestAttrs)
	instruments.WorkflowControlStoreSelections.Add(ctx, 1, metric.WithAttributes(AttrBackendKind("claim_plane")))

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	seen := map[string]metricdata.Aggregation{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			seen[m.Name] = m.Data
		}
	}
	for _, name := range []string{
		"eshu_dp_workflow_claim_plane_requests_total",
		"eshu_dp_workflow_claim_plane_request_duration_seconds",
		"eshu_dp_workflow_control_store_selections_total",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("metric %s not emitted; saw %v", name, claimPlaneMetricNames(seen))
		}
	}
	histogram, ok := seen["eshu_dp_workflow_claim_plane_request_duration_seconds"].(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("duration histogram data = %#v, want one float64 data point", seen["eshu_dp_workflow_claim_plane_request_duration_seconds"])
	}
	bounds := histogram.DataPoints[0].Bounds
	if len(bounds) == 0 || bounds[0] > 0.005 || bounds[len(bounds)-1] != 10 {
		t.Fatalf("duration bounds = %v, want second-scale bounds from <=5ms to the 10s client timeout", bounds)
	}
}

func claimPlaneMetricNames(m map[string]metricdata.Aggregation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
