// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSupplyChainImpactWinnersScopeRemainderConvertedCounter pins the #11145
// operator signal: NewInstruments registers
// eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total,
// a positive count adds to it, and a zero or negative count records nothing.
func TestSupplyChainImpactWinnersScopeRemainderConvertedCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, 5)
	inst.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, 0)
	inst.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, -3)
	inst.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, 2)
	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, 1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total" {
				continue
			}
			found = true
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("converted metric data type = %T, want Int64 sum", m.Data)
			}
			for _, dp := range sum.DataPoints {
				if dp.Attributes.Len() != 0 {
					t.Errorf("converted counter carries attributes %v, want none", dp.Attributes)
				}
				total += dp.Value
			}
		}
	}
	if !found {
		t.Fatal("scope-remainder converted counter is not registered")
	}
	if total != 7 {
		t.Fatalf("converted rows total = %d, want 7 (5 + 2; zero and negative counts record nothing)", total)
	}
}
