// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestStableKeyFillInstrumentsRecordBoundedLabelsAndAreNilSafe proves the four
// fill signals register, count under their closed label sets, ignore a zero row
// count, and are safe on a nil receiver and an unregistered Instruments.
func TestStableKeyFillInstrumentsRecordBoundedLabelsAndAreNilSafe(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactStableKeyFillInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactStableKeyFillStep(ctx, SupplyChainImpactStableKeyFillStepChunk)
	inst.RecordSupplyChainImpactStableKeyFillRows(ctx, SupplyChainImpactStableKeyFillRowsFilled, 7)
	inst.RecordSupplyChainImpactStableKeyFillRows(ctx, SupplyChainImpactStableKeyFillRowsSkipped, 0)
	inst.RecordSupplyChainImpactStableKeyFillPause(ctx, SupplyChainImpactStableKeyFillPauseReplayLag)
	inst.RecordSupplyChainImpactStableKeyFillComplete(ctx, true)
	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactStableKeyFillStep(ctx, SupplyChainImpactStableKeyFillStepChunk)
	nilInst.RecordSupplyChainImpactStableKeyFillRows(ctx, SupplyChainImpactStableKeyFillRowsFilled, 1)
	nilInst.RecordSupplyChainImpactStableKeyFillPause(ctx, SupplyChainImpactStableKeyFillPauseFreeDisk)
	nilInst.RecordSupplyChainImpactStableKeyFillComplete(ctx, true)
	(&Instruments{}).RecordSupplyChainImpactStableKeyFillStep(ctx, SupplyChainImpactStableKeyFillStepGap)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					label := ""
					for _, kv := range point.Attributes.ToSlice() {
						label = kv.Value.AsString()
					}
					got[m.Name+"/"+label] += point.Value
				}
			case metricdata.Gauge[int64]:
				for _, point := range data.DataPoints {
					got[m.Name] += point.Value
				}
			}
		}
	}
	for key, want := range map[string]int64{
		"eshu_dp_supply_chain_impact_stable_key_fill_steps_total/chunk":       1,
		"eshu_dp_supply_chain_impact_stable_key_fill_rows_total/filled":       7,
		"eshu_dp_supply_chain_impact_stable_key_fill_pauses_total/replay_lag": 1,
		"eshu_dp_supply_chain_impact_stable_key_fill_complete":                1,
	} {
		if got[key] != want {
			t.Errorf("%s = %d, want %d (all: %v)", key, got[key], want, got)
		}
	}
	if _, ok := got["eshu_dp_supply_chain_impact_stable_key_fill_rows_total/skipped"]; ok {
		t.Error("a zero skipped row count recorded a sample")
	}
}
