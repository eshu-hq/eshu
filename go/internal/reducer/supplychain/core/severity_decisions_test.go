// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestEmitSeverityDecisionsCountsOneOutcomePerFinding pins the #9693 operator
// signal: eshu_dp_supply_chain_severity_decisions_total carries exactly the
// closed outcome set, one count per finding, and no label beyond domain and
// outcome.
func TestEmitSeverityDecisionsCountsOneOutcomePerFinding(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := SupplyChainImpactHandler{Instruments: inst}
	handler.emitSeverityDecisions(context.Background(), []SupplyChainImpactFinding{
		{CVSSScore: 7.5},
		{CVSSScore: 7.5},
		{CVSSScore: 9.8, SeverityScoreSource: "derived_from_vector"},
		{SeverityLabel: "MODERATE"},
		{SeverityLabel: "severe"},
		{},
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_severity_decisions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("data point labels = %v, want only domain and outcome", point.Attributes)
				}
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				got[outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"source_reported": 2, "derived_from_vector": 1, "label_only": 1, "none": 2}
	for outcome, count := range want {
		if got[outcome] != count {
			t.Errorf("outcome %s = %d, want %d (all: %v)", outcome, got[outcome], count, got)
		}
	}
}
