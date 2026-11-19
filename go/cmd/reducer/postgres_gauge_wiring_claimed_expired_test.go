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

// The production store must keep providing the reclaim-backlog readings.
var _ telemetry.ReducerClaimedExpiredObserver = (*postgres.QueueObserverStore)(nil)

// scriptedClaimedExpiredStore is the scripted gauge store plus #10865
// readings.
type scriptedClaimedExpiredStore struct {
	scriptedPostgresGauges
	calls int
}

func (s *scriptedClaimedExpiredStore) ReducerClaimedExpiredCounts(context.Context) (int64, float64, error) {
	s.calls++
	return 6, 18240, nil
}

// TestPostgresBackedGaugesServeClaimedExpiredSnapshot proves the #10865
// gauges serve from the background snapshot: scrapes observe the refreshed
// values without a Postgres read per scrape.
func TestPostgresBackedGaugesServeClaimedExpiredSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	store := &scriptedClaimedExpiredStore{}
	instruments := &telemetry.Instruments{}
	refresher, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), store, nil, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.ReducerClaimedExpired == nil {
		t.Fatal("claimed-expired gauges not registered for a claimed-expired-capable store")
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(3 * time.Second)
	var rm metricdata.ResourceMetrics
	for claimedExpiredCount(&rm) != 6 && time.Now().Before(deadline) {
		rm = metricdata.ResourceMetrics{}
		// Before the first snapshot publishes, the scalar callbacks report
		// not-ready and Collect aggregates that error; keep polling.
		_ = reader.Collect(context.Background(), &rm)
		time.Sleep(5 * time.Millisecond)
	}
	if got := claimedExpiredCount(&rm); got != 6 {
		t.Fatalf("eshu_dp_reducer_claimed_expired = %d, want 6", got)
	}
	if got := claimedExpiredAge(&rm); got != 18240 {
		t.Fatalf("eshu_dp_reducer_claimed_expired_oldest_age_seconds = %v, want 18240", got)
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

// TestClaimedExpiredGaugesAbsentWithoutObserver proves a store without the
// reclaim-backlog contract registers no claimed-expired gauges.
func TestClaimedExpiredGaugesAbsentWithoutObserver(t *testing.T) {
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments := &telemetry.Instruments{}
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), scriptedPostgresGauges{}, nil, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.ReducerClaimedExpired != nil {
		t.Fatal("claimed-expired gauges registered without a claimed-expired observer")
	}
}

func claimedExpiredCount(rm *metricdata.ResourceMetrics) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_reducer_claimed_expired" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok || len(gauge.DataPoints) != 1 {
				continue
			}
			return gauge.DataPoints[0].Value
		}
	}
	return -1
}

func claimedExpiredAge(rm *metricdata.ResourceMetrics) float64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_reducer_claimed_expired_oldest_age_seconds" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok || len(gauge.DataPoints) != 1 {
				continue
			}
			return gauge.DataPoints[0].Value
		}
	}
	return -1
}
