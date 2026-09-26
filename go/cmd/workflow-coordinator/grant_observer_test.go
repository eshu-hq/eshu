// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// TestNewGrantObserverRecordsOnProcessInstruments proves the observer the
// coordinator wires records on the process's own instruments, so a decision
// reaches the eshu_dp_component_producer_grant_decisions_total counter.
func TestNewGrantObserverRecordsOnProcessInstruments(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	newGrantObserver(inst, nil).ObserveGrantDecision(context.Background(), component.GrantDecision{
		Stage:      component.GrantStageReadback,
		Allowed:    false,
		Reason:     component.GrantReasonRevoked,
		ProducerID: "dev.example.collector.pick",
		Version:    "0.1.0",
		Kind:       "aws_resource",
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != contract.MetricProducerGrantDecisions {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += dp.Value
			}
		}
	}
	if total != 1 {
		t.Fatalf("%s total = %d, want 1", contract.MetricProducerGrantDecisions, total)
	}
}
