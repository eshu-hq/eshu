// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// TestVulnerabilitySourceStateInstrumentNamesMatchContract pins the literal
// metric names (kept literal for the telemetry-coverage verifier) to the
// contract constants operators and docs use (#9420).
func TestVulnerabilitySourceStateInstrumentNamesMatchContract(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerVulnerabilitySourceStateInstruments(meter, inst); err != nil {
		t.Fatalf("registerVulnerabilitySourceStateInstruments() error = %v", err)
	}
	ctx := context.Background()
	inst.VulnerabilitySourceStateStaleWritesSkipped.Add(ctx, 1)
	inst.VulnerabilitySourceStateWriteFailures.Add(ctx, 1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]bool{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			got[m.Name] = true
		}
	}
	for _, name := range []string{
		contract.MetricVulnerabilitySourceStateStaleWritesSkipped,
		contract.MetricVulnerabilitySourceStateWriteFailures,
	} {
		if !got[name] {
			t.Fatalf("metric %q not registered; got %v", name, got)
		}
	}
}
