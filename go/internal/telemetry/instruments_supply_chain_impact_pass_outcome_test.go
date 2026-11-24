// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSupplyChainImpactPassOutcomeLabelSetsAreClosed pins the #10972 label
// value sets: each is a small closed enum, with no duplicate and no empty
// member, so the counter's series count is bounded by their product.
func TestSupplyChainImpactPassOutcomeLabelSetsAreClosed(t *testing.T) {
	t.Parallel()

	causes := SupplyChainImpactRependCauses()
	wantCauses := []string{"first", "retry", "drain", "fanout_rerun", "other_repend", "unknown"}
	if !slices.Equal(causes, wantCauses) {
		t.Fatalf("SupplyChainImpactRependCauses() = %v, want %v", causes, wantCauses)
	}
	changes := SupplyChainImpactPassChanges()
	if want := []string{"changed", "unchanged"}; !slices.Equal(changes, want) {
		t.Fatalf("SupplyChainImpactPassChanges() = %v, want %v", changes, want)
	}
	classes := SupplyChainImpactChangeClasses()
	if want := []string{"none", "unknown"}; !slices.Equal(classes, want) {
		t.Fatalf("SupplyChainImpactChangeClasses() = %v, want %v", classes, want)
	}
	for name, set := range map[string][]string{"cause": causes, "changed": changes, "class": classes} {
		seen := map[string]struct{}{}
		for _, value := range set {
			if value == "" {
				t.Fatalf("%s set has an empty member", name)
			}
			if _, dup := seen[value]; dup {
				t.Fatalf("%s set repeats %q", name, value)
			}
			seen[value] = struct{}{}
		}
	}
}

// TestSupplyChainImpactPassOutcomeInstrumentExportsBoundedLabels records
// through the registered instrument and reads it back: one monotonic int64
// sum whose only label keys are domain, mode, repend_cause, changed and
// change_class. No scope, generation, intent or CVE id is a label.
func TestSupplyChainImpactPassOutcomeInstrumentExportsBoundedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("pass-outcome-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if inst.SupplyChainImpactPassOutcomeTotal == nil {
		t.Fatal("SupplyChainImpactPassOutcomeTotal is not registered")
	}
	ctx := context.Background()
	inst.SupplyChainImpactPassOutcomeTotal.Add(ctx, 1, metric.WithAttributes(
		AttrDomain("supply_chain_impact"),
		AttrSupplyChainImpactPassMode("full"),
		AttrSupplyChainImpactRependCause(SupplyChainImpactRependCauseFanOutRerun),
		AttrSupplyChainImpactPassChanged(SupplyChainImpactPassUnchanged),
		AttrSupplyChainImpactChangeClass(SupplyChainImpactChangeClassNone),
	))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var found *metricdata.Metrics
	for _, scope := range rm.ScopeMetrics {
		for i := range scope.Metrics {
			if scope.Metrics[i].Name == "eshu_dp_supply_chain_impact_pass_outcome_total" {
				found = &scope.Metrics[i]
			}
		}
	}
	if found == nil {
		t.Fatal("eshu_dp_supply_chain_impact_pass_outcome_total not exported")
	}
	sum, ok := found.Data.(metricdata.Sum[int64])
	if !ok || !sum.IsMonotonic || len(sum.DataPoints) != 1 {
		t.Fatalf("data = %#v, want one monotonic int64 sum point", found.Data)
	}
	var keys []string
	for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
		keys = append(keys, string(kv.Key))
	}
	sort.Strings(keys)
	want := []string{"change_class", "changed", "domain", "mode", "repend_cause"}
	if !slices.Equal(keys, want) {
		t.Fatalf("label keys = %v, want %v", keys, want)
	}
}
