// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeSupplyChainImpactDeadLetterObserver struct {
	counts []SupplyChainImpactDeadLetterCount
	err    error
}

func (f fakeSupplyChainImpactDeadLetterObserver) SupplyChainImpactActiveDeadLetters(context.Context) ([]SupplyChainImpactDeadLetterCount, error) {
	return f.counts, f.err
}

// deadLetterGaugePoints collects the two #11147 gauges as name to
// failure_class to value.
func deadLetterGaugePoints(t *testing.T, obs SupplyChainImpactDeadLetterObserver) map[string]map[string]int64 {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst, err := NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if err := RegisterSupplyChainImpactDeadLetterObservableGauges(inst, meter, obs); err != nil {
		t.Fatalf("register error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil && obs != nil {
		// A failing callback reports its error here; the points still say
		// what was observed.
		t.Logf("Collect() = %v", err)
	}
	out := map[string]map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				class, _ := dp.Attributes.Value(MetricDimensionFailureClass)
				if out[m.Name] == nil {
					out[m.Name] = map[string]int64{}
				}
				out[m.Name][class.AsString()] = dp.Value
			}
		}
	}
	return out
}

// TestSupplyChainImpactDeadLetterGaugesObserveByClass proves the #11147
// gauges: one series per failure class for every active-generation dead
// letter, and a separate gauge for the subset that still owes a fan-out.
func TestSupplyChainImpactDeadLetterGaugesObserveByClass(t *testing.T) {
	t.Parallel()
	points := deadLetterGaugePoints(t, fakeSupplyChainImpactDeadLetterObserver{counts: []SupplyChainImpactDeadLetterCount{
		{FailureClass: "supply_chain_impact_fanout_failed", Total: 209, OwingFanOut: 200},
		{FailureClass: "reducer_handler_abandoned_at_shutdown", Total: 8, OwingFanOut: 8},
		{FailureClass: "reducer_handler_timeout", Total: 3},
	}})
	total := points[MetricSupplyChainImpactActiveDeadLetters]
	if total["supply_chain_impact_fanout_failed"] != 209 || total["reducer_handler_abandoned_at_shutdown"] != 8 || total["reducer_handler_timeout"] != 3 {
		t.Fatalf("total gauge = %v", total)
	}
	owing := points[MetricSupplyChainImpactActiveDeadLettersOwingFanOut]
	if len(owing) != 3 || owing["supply_chain_impact_fanout_failed"] != 200 || owing["reducer_handler_timeout"] != 0 {
		t.Fatalf("owing gauge = %v, want every class observed, zero included", owing)
	}
}

// TestSupplyChainImpactDeadLetterGaugesObserveNothingWithoutASample proves
// the gauges stay absent until a reading exists: a not-ready or failed read
// observes nothing (a zero would claim no dead letters), and a nil observer
// registers nothing.
func TestSupplyChainImpactDeadLetterGaugesObserveNothingWithoutASample(t *testing.T) {
	t.Parallel()
	if points := deadLetterGaugePoints(t, fakeSupplyChainImpactDeadLetterObserver{err: errors.New("not ready")}); len(points) != 0 {
		t.Fatalf("points = %v, want none before a sample", points)
	}
	if points := deadLetterGaugePoints(t, nil); len(points) != 0 {
		t.Fatalf("points = %v, want none with no observer", points)
	}
}
