// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestRecordSupplyChainImpactSupportsVerifySlice proves a verification slice
// lands on both registered instruments under its outcome: one count and one
// duration sample, with no other label.
func TestRecordSupplyChainImpactSupportsVerifySlice(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactSupportsVerifySlice(ctx, SupplyChainImpactSupportsVerifySliceAdvance, 4*time.Second)
	inst.RecordSupplyChainImpactSupportsVerifySlice(ctx, SupplyChainImpactSupportsVerifySliceAdvance, 5*time.Second)
	inst.RecordSupplyChainImpactSupportsVerifySlice(ctx, SupplyChainImpactSupportsVerifySliceGap, time.Second)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counts := map[string]int64{}
	samples := map[string]uint64{}
	sums := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total":
				for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
					counts[verifySliceOutcome(t, point.Attributes)] += point.Value
				}
			case "eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds":
				for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					outcome := verifySliceOutcome(t, point.Attributes)
					samples[outcome] += point.Count
					sums[outcome] += point.Sum
				}
			}
		}
	}
	if counts["advance"] != 2 || counts["gap"] != 1 || len(counts) != 2 {
		t.Fatalf("slice counts = %v, want advance=2 gap=1", counts)
	}
	if samples["advance"] != 2 || sums["advance"] != 9 || samples["gap"] != 1 || sums["gap"] != 1 {
		t.Fatalf("slice durations = counts %v sums %v, want advance 2 samples summing 9 s and gap 1 sample of 1 s", samples, sums)
	}

	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactSupportsVerifySlice(ctx, SupplyChainImpactSupportsVerifySliceLast, time.Second)
}

// verifySliceOutcome returns the outcome label of a point that carries only it.
func verifySliceOutcome(t *testing.T, set attribute.Set) string {
	t.Helper()
	if set.Len() != 1 {
		t.Fatalf("attributes = %v, want the outcome label alone", set.ToSlice())
	}
	value, ok := set.Value(attribute.Key("outcome"))
	if !ok {
		t.Fatalf("attributes = %v, want an outcome label", set.ToSlice())
	}
	return value.AsString()
}
