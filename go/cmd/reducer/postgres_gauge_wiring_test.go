// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// blockingPostgresQueueObserver stands in for a Postgres queue read that never
// returns until its context is done, the #7064 failure shape: a slow or locked
// Postgres read holding the metrics collection lock and wedging every scrape
// behind it.
type blockingPostgresQueueObserver struct {
	entered chan struct{}
}

func (o *blockingPostgresQueueObserver) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	select {
	case o.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (o *blockingPostgresQueueObserver) QueueOldestAge(context.Context) (map[string]float64, error) {
	return nil, nil
}

// TestPostgresQueueGaugesDoNotBlockMetricsCollection is the #7064 regression:
// a stuck Postgres queue read must never wedge the meter collection that
// /metrics scrapes drive.
func TestPostgresQueueGaugesDoNotBlockMetricsCollection(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	queue := &blockingPostgresQueueObserver{entered: make(chan struct{}, 1)}
	instruments := &telemetry.Instruments{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	refresher, err := registerPostgresBackedGauges(
		instruments, provider.Meter("test"), queue, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
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
		t.Fatal("Collect() blocked on a stuck Postgres read; Postgres gauges must not query on the scrape path")
	}
}

// scriptedPostgresGauges returns fixed readings for every Postgres gauge
// family so the snapshot round-trip can be pinned end to end.
type scriptedPostgresGauges struct{}

func (scriptedPostgresGauges) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return map[string]map[string]int64{"q1": {"pending": 3, "in_flight": 1}}, nil
}

func (scriptedPostgresGauges) QueueOldestAge(context.Context) (map[string]float64, error) {
	return map[string]float64{"q1": 1.5}, nil
}

func (scriptedPostgresGauges) SourceQueueDepths(context.Context) (map[string]map[string]map[string]int64, error) {
	return map[string]map[string]map[string]int64{"q1": {"s1": {"pending": 2}}}, nil
}

func (scriptedPostgresGauges) SourceQueueOldestAge(context.Context) (map[string]map[string]float64, error) {
	return map[string]map[string]float64{"q1": {"s1": 2.5}}, nil
}

func (scriptedPostgresGauges) AcceptanceRowCount(context.Context) (int64, error) {
	return 7, nil
}

func (scriptedPostgresGauges) WorkflowFamilyQueueDepths(context.Context) (map[string]map[string]map[string]int64, error) {
	return map[string]map[string]map[string]int64{"fam": {"src": {"pending": 4}}}, nil
}

func (scriptedPostgresGauges) ActiveGenerationsByAge(context.Context) (map[string]int64, error) {
	return map[string]int64{"fresh": 5, "stuck": 1}, nil
}

func (scriptedPostgresGauges) PoisonDeadLetterCounts(context.Context) (int64, int64, float64, error) {
	return 2, 9, 3.25, nil
}

// TestPostgresBackedGaugesServeRefreshedSnapshot proves the scrape serves the
// exact values the refresher read, across every Postgres gauge family,
// including the nested-map flattening and the float-age millis encoding.
func TestPostgresBackedGaugesServeRefreshedSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	scripted := scriptedPostgresGauges{}
	refresher, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"),
		scripted, scripted, scripted, scripted, scripted, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	wantInt := map[string]int64{
		"eshu_dp_queue_depth":                 3 + 1,
		"eshu_dp_queue_source_depth":          2,
		"eshu_dp_shared_acceptance_rows":      7,
		"eshu_dp_workflow_family_queue_depth": 4,
		"eshu_dp_active_generations":          5 + 1,
		"eshu_dp_poison_dead_letter_scopes":   2,
		"eshu_dp_poison_dead_letter_items":    9,
	}
	wantFloat := map[string]float64{
		"eshu_dp_queue_oldest_age_seconds":              1.5,
		"eshu_dp_queue_source_oldest_age_seconds":       2.5,
		"eshu_dp_poison_dead_letter_oldest_age_seconds": 3.25,
	}
	// Spot-check one fully-labeled point per nested family so the flattening
	// cannot silently drop a dimension.
	wantLabeled := map[string]map[string]string{
		"eshu_dp_queue_depth":                 {"queue": "q1", "status": "pending"},
		"eshu_dp_queue_source_depth":          {"queue": "q1", "source_system": "s1", "status": "pending"},
		"eshu_dp_workflow_family_queue_depth": {"collector_kind": "fam", "source_system": "src", "status": "pending"},
		"eshu_dp_active_generations":          {"age_bucket": "stuck"},
	}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for {
		var rm metricdata.ResourceMetrics
		// Scalar callbacks report the not-ready error until the first
		// snapshot publishes; Collect surfaces callback errors, so tolerate
		// them while polling for the series.
		lastErr = reader.Collect(context.Background(), &rm)
		gotInt := map[string]int64{}
		gotFloat := map[string]float64{}
		labeled := map[string]bool{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				switch data := m.Data.(type) {
				case metricdata.Gauge[int64]:
					for _, dp := range data.DataPoints {
						gotInt[m.Name] += dp.Value
						if attrs, ok := wantLabeled[m.Name]; ok && matchAttrs(dp.Attributes, attrs) {
							labeled[m.Name] = true
						}
					}
				case metricdata.Gauge[float64]:
					for _, dp := range data.DataPoints {
						gotFloat[m.Name] += dp.Value
					}
				}
			}
		}
		done := true
		for name, want := range wantInt {
			if gotInt[name] != want {
				done = false
			}
		}
		for name, want := range wantFloat {
			if gotFloat[name] != want {
				done = false
			}
		}
		for name := range wantLabeled {
			if !labeled[name] {
				done = false
			}
		}
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gauges after refresh: int=%v float=%v labeled=%v lastErr=%v", gotInt, gotFloat, labeled, lastErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func matchAttrs(set attribute.Set, want map[string]string) bool {
	for k, v := range want {
		got, ok := set.Value(attribute.Key(k))
		if !ok || got.AsString() != v {
			return false
		}
	}
	return true
}

// TestPostgresScalarGaugesReportNothingBeforeFirstRefresh proves the scalar
// gauges (acceptance rows, poison class) observe nothing — rather than a false
// zero — until the refresher publishes its first snapshot. A zero poison scope
// count means "class empty", so a pre-success zero would lie.
func TestPostgresScalarGaugesReportNothingBeforeFirstRefresh(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	getenv := func(string) string { return "" }
	scripted := scriptedPostgresGauges{}
	if _, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"),
		nil, scripted, nil, nil, scripted, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	// No refresher start: no snapshot has ever been published.
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "eshu_dp_shared_acceptance_rows",
				"eshu_dp_poison_dead_letter_scopes",
				"eshu_dp_poison_dead_letter_items",
				"eshu_dp_poison_dead_letter_oldest_age_seconds":
				t.Fatalf("scalar gauge %q reported before the first snapshot", m.Name)
			}
		}
	}
}

// TestTwoGaugeRefreshersShareSnapshotInstruments proves the production wiring
// is safe: the graph refresher (#7062) and the Postgres refresher (#7064)
// register the same eshu_dp_gauge_snapshot_* instruments on one meter. Each
// refresher serves its own sources, and both refresh metric families report.
func TestTwoGaugeRefreshersShareSnapshotInstruments(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }

	graph, err := snapshot.New(snapshot.Config{Meter: provider.Meter("test")})
	if err != nil {
		t.Fatalf("graph snapshot.New() error = %v", err)
	}
	if _, err := graph.Register("graph_probe", func(context.Context) (map[string]int64, error) {
		return map[string]int64{"Repository": 1}, nil
	}); err != nil {
		t.Fatalf("graph Register() error = %v", err)
	}
	postgres, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"),
		scriptedPostgresGauges{}, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	graph.Start(ctx)
	waitPostgres := startPostgresGaugeRefresher(ctx, postgres)
	t.Cleanup(func() { cancel(); graph.Wait(); waitPostgres() })

	deadline := time.Now().Add(5 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &rm)
		gauges := map[string]bool{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "eshu_dp_gauge_snapshot_refreshes_total" {
					for _, dp := range sum.DataPoints {
						if v, ok := dp.Attributes.Value("gauge"); ok {
							gauges[v.AsString()] = true
						}
					}
				}
			}
		}
		if gauges["graph_probe"] && gauges["reducer_queue_depth"] {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("refresh outcomes missing: have gauges %v", gauges)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
