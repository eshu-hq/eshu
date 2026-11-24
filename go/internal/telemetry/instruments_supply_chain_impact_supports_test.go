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

// TestRecordSupplyChainImpactFactWriteRejected proves the rejected-write
// counter counts one per call with no label (no scope, generation or fact id
// can reach it) and is safe on a nil receiver and on a bare Instruments.
func TestRecordSupplyChainImpactFactWriteRejected(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactFactWriteRejected(ctx)
	inst.RecordSupplyChainImpactFactWriteRejected(ctx)

	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactFactWriteRejected(ctx)
	(&Instruments{}).RecordSupplyChainImpactFactWriteRejected(ctx)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	var attributes int
	var found bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fact_write_rejected_total" {
				continue
			}
			found = true
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += point.Value
				attributes += point.Attributes.Len()
			}
		}
	}
	if !found {
		t.Fatal("eshu_dp_supply_chain_impact_fact_write_rejected_total is not registered")
	}
	if total != 2 || attributes != 0 {
		t.Fatalf("rejected counter = %d with %d attributes, want 2 with none", total, attributes)
	}
}

// TestRecordSupplyChainImpactStoredKeyMissing proves the stored-key-missing
// counter adds the rows it is given with no label, ignores zero and negative
// counts, and is safe on a nil receiver and on an unregistered Instruments.
func TestRecordSupplyChainImpactStoredKeyMissing(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactStoredKeyMissing(ctx, 3)
	inst.RecordSupplyChainImpactStoredKeyMissing(ctx, 2)
	inst.RecordSupplyChainImpactStoredKeyMissing(ctx, 0)
	inst.RecordSupplyChainImpactStoredKeyMissing(ctx, -4)
	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactStoredKeyMissing(ctx, 9)
	(&Instruments{}).RecordSupplyChainImpactStoredKeyMissing(ctx, 9)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	var attributes int
	var found bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_stored_key_missing_total" {
				continue
			}
			found = true
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += point.Value
				attributes += point.Attributes.Len()
			}
		}
	}
	if !found {
		t.Fatal("eshu_dp_supply_chain_impact_stored_key_missing_total is not registered")
	}
	if total != 5 || attributes != 0 {
		t.Fatalf("stored key missing counter = %d with %d attributes, want 5 with none", total, attributes)
	}
}

// TestRecordSupplyChainImpactFactWriteStopArmed pins the armed gauge: 1 while the trigger reads
// enabled, 0 after it reads disabled, no attributes (so no scope or generation id can reach it), and
// safe on a nil receiver and an Instruments that did not register it.
func TestRecordSupplyChainImpactFactWriteStopArmed(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactFactWriteStopArmed(ctx, true)
	(&Instruments{}).RecordSupplyChainImpactFactWriteStopArmed(ctx, true)

	read := func() (value int64, attributes int) {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		found := false
		for _, scope := range rm.ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name != "eshu_dp_supply_chain_impact_fact_write_stop_armed" {
					continue
				}
				found = true
				for _, point := range m.Data.(metricdata.Gauge[int64]).DataPoints {
					value, attributes = point.Value, point.Attributes.Len()
				}
			}
		}
		if !found {
			t.Fatal("eshu_dp_supply_chain_impact_fact_write_stop_armed was not recorded")
		}
		return value, attributes
	}
	inst.RecordSupplyChainImpactFactWriteStopArmed(ctx, true)
	if value, attributes := read(); value != 1 || attributes != 0 {
		t.Fatalf("armed gauge = %d with %d attributes, want 1 with none", value, attributes)
	}
	inst.RecordSupplyChainImpactFactWriteStopArmed(ctx, false)
	if value, _ := read(); value != 0 {
		t.Fatalf("armed gauge = %d after the trigger read disabled, want 0", value)
	}
}
