// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type blockingIngesterQueueObserver struct {
	entered chan struct{}
}

func (o *blockingIngesterQueueObserver) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	select {
	case o.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (o *blockingIngesterQueueObserver) QueueOldestAge(context.Context) (map[string]float64, error) {
	return nil, nil
}

// TestIngesterQueueGaugesDoNotBlockMetricsCollection is the #7064 regression
// for the ingester: a stuck Postgres queue read must never wedge the meter
// collection that /metrics scrapes drive.
func TestIngesterQueueGaugesDoNotBlockMetricsCollection(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	queue := &blockingIngesterQueueObserver{entered: make(chan struct{}, 1)}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	refresher, err := registerPostgresQueueGauges(
		&telemetry.Instruments{}, provider.Meter("test"), queue, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresQueueGauges() error = %v", err)
	}
	wait := startPostgresQueueGauges(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })
	select {
	case <-queue.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("refresher never started its Postgres read")
	}

	done := make(chan error, 1)
	go func() {
		var rm metricdata.ResourceMetrics
		done <- reader.Collect(context.Background(), &rm)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Collect() blocked on a stuck Postgres read; ingester queue gauges must not query on the scrape path")
	}
}

type scriptedIngesterQueueObserver struct{}

func (scriptedIngesterQueueObserver) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return map[string]map[string]int64{"collector": {"pending": 6}}, nil
}

func (scriptedIngesterQueueObserver) QueueOldestAge(context.Context) (map[string]float64, error) {
	return map[string]float64{"collector": 4.5}, nil
}

func (scriptedIngesterQueueObserver) SourceQueueDepths(context.Context) (map[string]map[string]map[string]int64, error) {
	return map[string]map[string]map[string]int64{"collector": {"git": {"pending": 2}}}, nil
}

func (scriptedIngesterQueueObserver) SourceQueueOldestAge(context.Context) (map[string]map[string]float64, error) {
	return map[string]map[string]float64{"collector": {"git": 1.25}}, nil
}

// TestIngesterQueueGaugesServeRefreshedSnapshot proves the ingester scrape
// serves the values the refresher read, including source-system dimensions
// and float ages.
func TestIngesterQueueGaugesServeRefreshedSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	refresher, err := registerPostgresQueueGauges(
		&telemetry.Instruments{}, provider.Meter("test"), scriptedIngesterQueueObserver{}, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresQueueGauges() error = %v", err)
	}
	wait := startPostgresQueueGauges(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(5 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &rm)
		var depth, sourceDepth int64
		var age, sourceAge float64
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				switch data := m.Data.(type) {
				case metricdata.Gauge[int64]:
					for _, dp := range data.DataPoints {
						switch m.Name {
						case "eshu_dp_queue_depth":
							depth += dp.Value
						case "eshu_dp_queue_source_depth":
							sourceDepth += dp.Value
						}
					}
				case metricdata.Gauge[float64]:
					for _, dp := range data.DataPoints {
						switch m.Name {
						case "eshu_dp_queue_oldest_age_seconds":
							age += dp.Value
						case "eshu_dp_queue_source_oldest_age_seconds":
							sourceAge += dp.Value
						}
					}
				}
			}
		}
		if depth == 6 && sourceDepth == 2 && age == 4.5 && sourceAge == 1.25 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gauges after refresh: depth=%d sourceDepth=%d age=%v sourceAge=%v", depth, sourceDepth, age, sourceAge)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
