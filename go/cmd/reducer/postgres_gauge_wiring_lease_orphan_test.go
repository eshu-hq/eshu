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

// The production store must keep providing the lease-orphan readings.
var _ telemetry.ReducerLeaseOrphanObserver = (*postgres.QueueObserverStore)(nil)

// scriptedLeaseOrphanStore is the scripted gauge store plus #11081 readings.
type scriptedLeaseOrphanStore struct {
	scriptedPostgresGauges
	calls int
}

func (s *scriptedLeaseOrphanStore) ReducerLeaseOrphanRows(context.Context) (int64, error) {
	s.calls++
	return 3, nil
}

// TestPostgresBackedGaugesServeLeaseOrphanSnapshot proves the #11081 gauge
// serves from the background snapshot: scrapes observe the refreshed value
// without a Postgres read per scrape.
func TestPostgresBackedGaugesServeLeaseOrphanSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	store := &scriptedLeaseOrphanStore{}
	instruments := &telemetry.Instruments{}
	refresher, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), store, nil, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.ReducerLeaseOrphanRows == nil {
		t.Fatal("lease-orphan gauge not registered for an orphan-capable store")
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(3 * time.Second)
	var rm metricdata.ResourceMetrics
	for leaseOrphanCount(&rm) != 3 && time.Now().Before(deadline) {
		rm = metricdata.ResourceMetrics{}
		// Before the first snapshot publishes, the scalar callbacks report
		// not-ready and Collect aggregates that error; keep polling.
		_ = reader.Collect(context.Background(), &rm)
		time.Sleep(5 * time.Millisecond)
	}
	if got := leaseOrphanCount(&rm); got != 3 {
		t.Fatalf("eshu_dp_reducer_lease_orphan_rows = %d, want 3", got)
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

// TestLeaseOrphanGaugeAbsentWithoutObserver proves a store without the
// orphan contract registers no lease-orphan gauge.
func TestLeaseOrphanGaugeAbsentWithoutObserver(t *testing.T) {
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments := &telemetry.Instruments{}
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), scriptedPostgresGauges{}, nil, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	if instruments.ReducerLeaseOrphanRows != nil {
		t.Fatal("lease-orphan gauge registered without an orphan observer")
	}
}

func leaseOrphanCount(rm *metricdata.ResourceMetrics) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_reducer_lease_orphan_rows" {
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
