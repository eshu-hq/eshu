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

// TestDiscoveryFailureInstrumentsDescribeTheirLabels pins #8084: both
// discovery failure instruments are registered with a description that names
// the operation label. The description is what a metrics backend shows an
// operator; the docs rows carry the list and mint values and the mint-only
// reset.
func TestDiscoveryFailureInstrumentsDescribeTheirLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("discovery-failures-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	attrs := metric.WithAttributes(AttrCollectorKind("git"), AttrOperation("mint"))
	inst.CollectorDiscoverySkippedCycles.Add(ctx, 1, attrs)
	inst.CollectorDiscoveryConsecutiveFailures.Record(ctx, 1, attrs)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	descriptions := map[string]string{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			descriptions[m.Name] = m.Description
		}
	}
	for name, want := range map[string]string{
		"eshu_dp_collector_discovery_skipped_cycles_total": "Total collector discovery cycles skipped after a retryable source failure instead of exiting the process, by collector_kind, operation and bounded failure_class",
		"eshu_dp_collector_discovery_consecutive_failures": "Consecutive collector discovery cycles that ended in a retryable failure, by collector_kind and operation; 0 after a successful selection",
	} {
		description, ok := descriptions[name]
		if !ok {
			t.Fatalf("metric %s not registered", name)
		}
		if description != want {
			t.Fatalf("%s description = %q, want %q", name, description, want)
		}
	}
}
