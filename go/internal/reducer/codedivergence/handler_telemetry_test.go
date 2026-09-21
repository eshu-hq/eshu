// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestHandleEmitsSharedDriftCounters proves the handler emits the shared
// correlation counters with the code_drifted pack: one rule_matches
// increment per evaluated pair (keyed by outcome reason), one
// drift_detected increment per admitted pair, and one rule_matches
// increment per budget-exhausted entity. No entity identity enters the
// label space.
func TestHandleEmitsSharedDriftCounters(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	admit := handlerPair("e1", "e2", "b.go", shingleSet(1, 9), append(shingleSet(1, 8), 101))
	low := handlerPair("e3", "e4", "c.go", shingleSet(1, 10), append(shingleSet(1, 6), 101, 102, 103, 104))
	loader := stubCandidateLoader{page: CandidatePage{
		Pairs: []CandidatePair{admit, low},
		Stats: CandidateStats{BudgetExhausted: []string{"e1"}},
	}}
	handler := CodeDriftedHandler{Loader: loader, Writer: &stubFindingWriter{}, Instruments: inst}
	if _, err := handler.Handle(context.Background(), driftedIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	// 2 evaluated pairs (admit + below-threshold) + 1 exhausted entity.
	if got := counterTotal(rm, "eshu_dp_correlation_rule_matches_total"); got != 3 {
		t.Fatalf("rule_matches = %d, want 3", got)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_drift_detected_total"); got != 1 {
		t.Fatalf("drift_detected = %d, want 1", got)
	}
	assertDriftCounterPack(t, rm, "eshu_dp_correlation_rule_matches_total")
	assertDriftCounterPack(t, rm, "eshu_dp_correlation_drift_detected_total")
}

func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// assertDriftCounterPack proves every drifted data point carries exactly the
// bounded pack/rule(/drift_kind) labels: no entity, path, or similarity
// value enters the label space.
func assertDriftCounterPack(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	allowed := map[string]bool{
		telemetry.MetricDimensionPack:      true,
		telemetry.MetricDimensionRule:      true,
		telemetry.MetricDimensionDriftKind: true,
	}
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				pack := ""
				for _, attr := range dp.Attributes.ToSlice() {
					if !allowed[string(attr.Key)] {
						t.Fatalf("%s carries unbounded label %q", name, string(attr.Key))
					}
					if string(attr.Key) == telemetry.MetricDimensionPack {
						pack = attr.Value.AsString()
					}
				}
				if pack != DriftedPack {
					t.Fatalf("%s pack = %q, want %q", name, pack, DriftedPack)
				}
			}
		}
	}
}
