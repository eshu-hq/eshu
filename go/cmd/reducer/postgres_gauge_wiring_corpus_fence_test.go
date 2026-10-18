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

// The production store must keep providing the corpus-fence readings.
var _ telemetry.CorpusFenceObserver = (*postgres.QueueObserverStore)(nil)

// scriptedCorpusFenceStore is the scripted gauge store plus #8247 readings.
type scriptedCorpusFenceStore struct {
	scriptedPostgresGauges
	calls int
}

func (s *scriptedCorpusFenceStore) CorpusFenceCounts(context.Context) (int64, map[string]int64, map[string]float64, error) {
	s.calls++
	return 3,
		map[string]int64{"deployable_unit_correlation": 782},
		map[string]float64{"deployable_unit_correlation": 90.5},
		nil
}

// TestPostgresBackedGaugesServeCorpusFenceSnapshot proves the #8247 gauges
// serve from the background snapshot: scrapes observe the refreshed values
// without a Postgres read per scrape.
func TestPostgresBackedGaugesServeCorpusFenceSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	store := &scriptedCorpusFenceStore{}
	instruments := &telemetry.Instruments{}
	refresher, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), store, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.CorpusFenceHolders == nil {
		t.Fatal("corpus fence gauges not registered for a fence-capable store")
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	var rm metricdata.ResourceMetrics
	deadline := time.Now().Add(3 * time.Second)
	for corpusFenceHolders(&rm) != 3 && time.Now().Before(deadline) {
		rm = metricdata.ResourceMetrics{}
		// Before the first snapshot publishes, the scalar callbacks report
		// not-ready and Collect aggregates that error; keep polling.
		_ = reader.Collect(context.Background(), &rm)
		time.Sleep(5 * time.Millisecond)
	}
	if got := corpusFenceHolders(&rm); got != 3 {
		t.Fatalf("eshu_dp_corpus_fence_holders = %d, want 3", got)
	}
	if got := corpusFenceInt(&rm, "eshu_dp_corpus_fence_deferred_consumers", "deployable_unit_correlation"); got != 782 {
		t.Fatalf("deferred = %d, want 782", got)
	}
	if got := corpusFenceFloat(&rm, "eshu_dp_corpus_fence_oldest_deferred_age_seconds", "deployable_unit_correlation"); got != 90.5 {
		t.Fatalf("oldest deferred age = %v, want 90.5", got)
	}
	// A second scrape must serve the snapshot, not re-read the store.
	before := store.calls
	rm2 := metricdata.ResourceMetrics{}
	if err := reader.Collect(context.Background(), &rm2); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if store.calls != before {
		t.Fatalf("scrape re-read the store: calls %d -> %d", before, store.calls)
	}
}

// TestCorpusFenceGaugesAbsentWithoutObserver proves a store without the
// corpus-fence contract registers no fence gauges.
func TestCorpusFenceGaugesAbsentWithoutObserver(t *testing.T) {
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments := &telemetry.Instruments{}
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), scriptedPostgresGauges{}, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.CorpusFenceHolders != nil {
		t.Fatal("corpus fence gauges registered without a corpus-fence observer")
	}
}

func corpusFenceHolders(rm *metricdata.ResourceMetrics) int64 {
	return corpusFenceInt(rm, "eshu_dp_corpus_fence_holders", "")
}

func corpusFenceInt(rm *metricdata.ResourceMetrics, name, domain string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				value, _ := dp.Attributes.Value("domain")
				if value.AsString() == domain {
					return dp.Value
				}
			}
		}
	}
	return -1
}

func corpusFenceFloat(rm *metricdata.ResourceMetrics, name, domain string) float64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				value, _ := dp.Attributes.Value("domain")
				if value.AsString() == domain {
					return dp.Value
				}
			}
		}
	}
	return -1
}
