// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The production store must keep providing the #11147 dead-letter readings.
var _ telemetry.SupplyChainImpactDeadLetterObserver = (*postgres.QueueObserverStore)(nil)

// scriptedDeadLetterStore is the scripted gauge store plus #11147 readings.
type scriptedDeadLetterStore struct {
	scriptedPostgresGauges
	calls int
}

func (s *scriptedDeadLetterStore) SupplyChainImpactActiveDeadLetters(context.Context) ([]telemetry.SupplyChainImpactDeadLetterCount, error) {
	s.calls++
	return []telemetry.SupplyChainImpactDeadLetterCount{
		{FailureClass: "supply_chain_impact_fanout_failed", Total: 209, OwingFanOut: 200},
		{FailureClass: "reducer_handler_timeout", Total: 3},
	}, nil
}

// TestPostgresBackedGaugesServeDeadLetterSnapshot proves the #11147 gauges
// serve from the background snapshot: the per-class totals and owing counts
// survive the flat snapshot round trip, and a second scrape does not read the
// store again.
func TestPostgresBackedGaugesServeDeadLetterSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	store := &scriptedDeadLetterStore{}
	refresher, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"), store, nil, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(3 * time.Second)
	var points map[string]map[string]int64
	for time.Now().Before(deadline) {
		var rm metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &rm)
		points = deadLetterGauges(&rm)
		if len(points[telemetry.MetricSupplyChainImpactActiveDeadLetters]) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	total := points[telemetry.MetricSupplyChainImpactActiveDeadLetters]
	owing := points[telemetry.MetricSupplyChainImpactActiveDeadLettersOwingFanOut]
	if total["supply_chain_impact_fanout_failed"] != 209 || total["reducer_handler_timeout"] != 3 {
		t.Fatalf("total gauge = %v", total)
	}
	if owing["supply_chain_impact_fanout_failed"] != 200 || len(owing) != 2 {
		t.Fatalf("owing gauge = %v", owing)
	}
	before := store.calls
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if store.calls != before {
		t.Fatalf("scrape re-read the store: calls %d -> %d", before, store.calls)
	}
}

// TestDeadLetterGaugesAbsentWithoutObserver proves a store without the
// dead-letter contract emits no dead-letter series.
func TestDeadLetterGaugesAbsentWithoutObserver(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"), scriptedPostgresGauges{}, nil, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	if points := deadLetterGauges(&rm); len(points) != 0 {
		t.Fatalf("dead-letter series = %v, want none", points)
	}
}

func deadLetterGauges(rm *metricdata.ResourceMetrics) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != telemetry.MetricSupplyChainImpactActiveDeadLetters && m.Name != telemetry.MetricSupplyChainImpactActiveDeadLettersOwingFanOut {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				class, _ := dp.Attributes.Value(telemetry.MetricDimensionFailureClass)
				if out[m.Name] == nil {
					out[m.Name] = map[string]int64{}
				}
				out[m.Name][class.AsString()] = dp.Value
			}
		}
	}
	return out
}
