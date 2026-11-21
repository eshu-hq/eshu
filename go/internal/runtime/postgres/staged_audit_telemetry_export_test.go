// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestStagedAuditTelemetryExportsAsMetrics pins the #11117 export (arbiter
// addendum item 5b): the staged audit's deferrals by reason and the last
// tick's batches, slowest batch, staged facts and distinct keys reach OTEL,
// not only the in-memory recorder.
func TestStagedAuditTelemetryExportsAsMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	recorder := NewStagedAuditTelemetry()
	if err := telemetry.RegisterSupplyChainImpactWinnersStagedAuditInstruments(meter, recorder); err != nil {
		t.Fatalf("register: %v", err)
	}
	recorder.ObserveDeferral(StagedAuditDeferBudget)
	recorder.ObserveDeferral(StagedAuditDeferBudget)
	recorder.ObserveDeferral(StagedAuditDeferConflictExhausted)
	recorder.ObserveTick(StagedAuditTickResult{Batches: 11539, MaxBatchDuration: 1500 * time.Millisecond, StagedFacts: 900000, DistinctKeys: 81000})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	ints := map[string]int64{}
	floats := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					key := m.Name
					if r, ok := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason)); ok {
						key += "{" + r.AsString() + "}"
					}
					ints[key] = dp.Value
				}
			case metricdata.Gauge[int64]:
				for _, dp := range data.DataPoints {
					ints[m.Name] = dp.Value
				}
			case metricdata.Gauge[float64]:
				for _, dp := range data.DataPoints {
					floats[m.Name] = dp.Value
				}
			}
		}
	}
	for name, want := range map[string]int64{
		"eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{budget}":             2,
		"eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{conflict_exhausted}": 1,
		"eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{batch_abort}":        0,
		"eshu_dp_supply_chain_impact_winners_staged_audit_ticks_total":                         1,
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_batches":                        11539,
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_staged_facts":                   900000,
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_distinct_keys":                  81000,
	} {
		if got, ok := ints[name]; !ok || got != want {
			t.Errorf("%s = %d (present %v), want %d", name, got, ok, want)
		}
	}
	if got := floats["eshu_dp_supply_chain_impact_winners_staged_audit_last_max_batch_seconds"]; got != 1.5 {
		t.Errorf("last max batch seconds = %v, want 1.5", got)
	}
}
