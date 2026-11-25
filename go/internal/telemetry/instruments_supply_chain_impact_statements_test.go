// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestWriteTxStatementTimingsRecordWithClosedLabels proves the #11430
// per-statement histogram: one observation per entry under exactly the
// statement and outcome labels, the closed 13-statement set with the exact
// spellings, buckets from sub-millisecond grants to the 300 s ceiling, and a
// safe no-op on a nil receiver, an empty Instruments, or an Instruments that
// did not register the instrument.
func TestWriteTxStatementTimingsRecordWithClosedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register supports: %v", err)
	}
	if err := registerSupplyChainImpactWriteTxStatementInstruments(meter, inst); err != nil {
		t.Fatalf("register statements: %v", err)
	}
	if inst.SupplyChainImpactWriteTxStatementDuration == nil {
		t.Fatal("SupplyChainImpactWriteTxStatementDuration is not registered")
	}

	ctx := context.Background()
	committed := make([]SupplyChainImpactWriteTxStatementTiming, 0, len(SupplyChainImpactWriteTxStatementLabels()))
	for _, statement := range SupplyChainImpactWriteTxStatementLabels() {
		committed = append(committed, SupplyChainImpactWriteTxStatementTiming{Statement: statement, Seconds: 0.012})
	}
	inst.RecordSupplyChainImpactWriteTxStatements(ctx, "committed", committed)
	inst.RecordSupplyChainImpactWriteTxStatements(ctx, "rolled_back", []SupplyChainImpactWriteTxStatementTiming{
		{Statement: SupplyChainImpactWriteTxStatementConflictLock, Seconds: 0.001},
		{Statement: SupplyChainImpactWriteTxStatementCommit, Seconds: 0},
	})
	inst.RecordSupplyChainImpactWriteTxStatements(ctx, "committed", nil)

	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactWriteTxStatements(ctx, "committed", committed)
	(&Instruments{}).RecordSupplyChainImpactWriteTxStatements(ctx, "committed", committed)
	(&Instruments{SupplyChainImpactWriteTxDuration: inst.SupplyChainImpactWriteTxDuration}).
		RecordSupplyChainImpactWriteTxStatements(ctx, "committed", committed)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	byStatement := map[string]map[string]uint64{}
	var buckets []float64
	found := false
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds" {
				continue
			}
			found = true
			for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("statement point has %d attributes, want exactly statement and outcome", point.Attributes.Len())
				}
				statement, ok := point.Attributes.Value(MetricDimensionStatement)
				if !ok {
					t.Fatalf("statement point lacks the %q label", MetricDimensionStatement)
				}
				outcome, ok := point.Attributes.Value(MetricDimensionOutcome)
				if !ok {
					t.Fatalf("statement point lacks the %q label", MetricDimensionOutcome)
				}
				if byStatement[statement.AsString()] == nil {
					byStatement[statement.AsString()] = map[string]uint64{}
				}
				byStatement[statement.AsString()][outcome.AsString()] += point.Count
				buckets = point.Bounds
			}
		}
	}
	if !found {
		t.Fatal("statement histogram has no points")
	}
	if got := len(byStatement); got != len(SupplyChainImpactWriteTxStatementLabels()) {
		t.Fatalf("statement series = %d, want %d (%v)", got, len(SupplyChainImpactWriteTxStatementLabels()), byStatement)
	}
	for _, statement := range SupplyChainImpactWriteTxStatementLabels() {
		byOutcome, ok := byStatement[statement]
		if !ok {
			t.Fatalf("no series for %q", statement)
		}
		if byOutcome["committed"] != 1 {
			t.Fatalf("statement %q committed count = %v, want 1", statement, byOutcome)
		}
	}
	if byStatement[SupplyChainImpactWriteTxStatementConflictLock]["rolled_back"] != 1 ||
		byStatement[SupplyChainImpactWriteTxStatementCommit]["rolled_back"] != 1 {
		t.Fatalf("rolled_back series = %v, want one rolled_back observation for conflict_lock and commit", byStatement)
	}
	if len(buckets) == 0 || buckets[0] > 0.001 {
		t.Fatalf("statement buckets %v do not reach sub-millisecond grants", buckets)
	}
	if buckets[len(buckets)-1] < 300 {
		t.Fatalf("statement buckets %v do not reach the 300 s ceiling", buckets)
	}
	if !slices.Equal(SupplyChainImpactWriteTxStatementLabels(), []string{
		"conflict_lock", "suppression_diff", "admission", "stored_read",
		"fact_insert", "supports_upsert", "winners_capture", "retract",
		"supports_delete", "recovery_read", "pass_state", "go_evidence_close",
		"commit",
	}) {
		t.Fatalf("statement labels = %q, want the closed 13-statement set", SupplyChainImpactWriteTxStatementLabels())
	}
	if MetricDimensionStatement != "statement" {
		t.Fatalf("MetricDimensionStatement = %q, want %q", MetricDimensionStatement, "statement")
	}
	if AttrStatement("commit").Value.AsString() != "commit" || string(AttrStatement("commit").Key) != MetricDimensionStatement {
		t.Fatal("AttrStatement does not carry the statement dimension")
	}

	// The nil and unregistered calls above must record nothing: the only
	// points are the registered committed and rolled-back ones.
	var total uint64
	for _, byOutcome := range byStatement {
		for _, count := range byOutcome {
			total += count
		}
	}
	if total != uint64(len(SupplyChainImpactWriteTxStatementLabels())+2) {
		t.Fatalf("statement observations = %d, want %d (nil and unregistered calls record nothing)", total, len(SupplyChainImpactWriteTxStatementLabels())+2)
	}
}
