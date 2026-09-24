// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// blockingGraphReader stands in for a Bolt read that never returns until its
// context is done, the ops-qa failure shape in #7062.
type blockingGraphReader struct {
	entered chan struct{}
}

func (r *blockingGraphReader) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *blockingGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// TestProvenanceGaugesDoNotBlockMetricsCollection is the #7062 regression: a
// stuck graph read must never wedge the meter collection that /metrics scrapes
// drive.
func TestProvenanceGaugesDoNotBlockMetricsCollection(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	graph := &blockingGraphReader{entered: make(chan struct{}, 1)}
	instruments := &telemetry.Instruments{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(key string) string {
		if key == graphGaugeRefreshTimeoutEnv {
			return "50ms"
		}
		return ""
	}
	refresher, err := registerGraphBackedGauges(instruments, provider.Meter("test"), graph, nil, getenv, nil)
	if err != nil {
		t.Fatalf("registerGraphBackedGauges() error = %v", err)
	}
	wait := startGraphGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })
	select {
	case <-graph.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("refresher never started its graph read")
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
		t.Fatal("Collect() blocked on a stuck graph read; provenance gauges must not read the graph on the scrape path")
	}
}

// TestProvenanceGaugeRefreshTimeoutIsRecorded proves the stuck read is bounded
// by the refresh deadline and surfaces as a timeout outcome an operator can
// alert on, while collection keeps answering.
func TestProvenanceGaugeRefreshTimeoutIsRecorded(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	graph := &blockingGraphReader{entered: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(key string) string {
		if key == graphGaugeRefreshTimeoutEnv {
			return "20ms"
		}
		if key == graphGaugeRefreshIntervalEnv {
			return "1h"
		}
		return ""
	}
	refresher, err := registerGraphBackedGauges(&telemetry.Instruments{}, provider.Meter("test"), graph, nil, getenv, nil)
	if err != nil {
		t.Fatalf("registerGraphBackedGauges() error = %v", err)
	}
	wait := startGraphGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(5 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		if timeoutOutcomes(rm, gaugeEdgesBySourceTool) >= 1 && timeoutOutcomes(rm, gaugeFilesByLanguage) >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for timeout outcomes on both provenance gauges")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func timeoutOutcomes(rm metricdata.ResourceMetrics, gauge string) int64 {
	return refreshOutcomes(rm, gauge, "timeout")
}

func refreshOutcomes(rm metricdata.ResourceMetrics, gauge, outcome string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_gauge_snapshot_refreshes_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				g, _ := dp.Attributes.Value("gauge")
				o, _ := dp.Attributes.Value("outcome")
				if g.AsString() == gauge && o.AsString() == outcome {
					return dp.Value
				}
			}
		}
	}
	return 0
}

type countingGraphReader struct{}

func (countingGraphReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "(f:File)") {
		return []map[string]any{{"language": "go", "cnt": int64(9)}}, nil
	}
	return []map[string]any{{"source_tool": "terraform", "cnt": int64(5)}}, nil
}

func (r countingGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

type fakeOrphanObserver struct{}

func (fakeOrphanObserver) GraphOrphanNodeCounts(context.Context) (map[string]int64, error) {
	return map[string]int64{"Repository": 2}, nil
}

// TestGraphBackedGaugesServeRefreshedSnapshot proves the scrape serves the
// values the refresher read, for the provenance gauges and the orphan gauge.
func TestGraphBackedGaugesServeRefreshedSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	refresher, err := registerGraphBackedGauges(&telemetry.Instruments{}, provider.Meter("test"), countingGraphReader{}, fakeOrphanObserver{}, getenv, nil)
	if err != nil {
		t.Fatalf("registerGraphBackedGauges() error = %v", err)
	}
	wait := startGraphGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	want := map[string]struct {
		label string
		value int64
	}{
		"eshu_dp_edges_by_source_tool": {"terraform", 35}, // 5 per verb x 7 verbs
		"eshu_dp_files_by_language":    {"go", 9},
		"eshu_dp_graph_orphan_nodes":   {"Repository", 2},
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		got := map[string]int64{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				data, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					continue
				}
				if w, tracked := want[m.Name]; tracked {
					for _, dp := range data.DataPoints {
						for _, kv := range dp.Attributes.ToSlice() {
							if kv.Value.AsString() == w.label {
								got[m.Name] = dp.Value
							}
						}
					}
				}
			}
		}
		done := true
		for name, w := range want {
			if got[name] != w.value {
				done = false
			}
		}
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gauges after refresh = %v, want values %v", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestGraphBackedGaugesSkipWithoutGraphPorts pins the no-op for binaries with
// neither a graph reader nor an orphan observer.
func TestGraphBackedGaugesSkipWithoutGraphPorts(t *testing.T) {
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	refresher, err := registerGraphBackedGauges(&telemetry.Instruments{}, provider.Meter("test"), nil, nil, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatalf("registerGraphBackedGauges(nil ports) error = %v", err)
	}
	if refresher != nil {
		t.Fatal("registerGraphBackedGauges(nil ports) returned a refresher, want nil")
	}
	// Starting and waiting on the nil refresher must be a safe no-op.
	startGraphGaugeRefresher(context.Background(), refresher)()
}

// closingGraphReader fails with a non-context error once the run context is
// cancelled, the way a graph driver being closed at shutdown fails an in-flight
// read (#7062 review F1).
type closingGraphReader struct{ entered chan struct{} }

func (r *closingGraphReader) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, errors.New("driver closed")
}

func (r *closingGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// TestGraphGaugeRefresherStopsWithRunContext proves cancelling the context the
// reducer runs under stops the refresh loops, and that a read cut short by that
// shutdown is not counted or logged as a refresh failure.
func TestGraphGaugeRefresherStopsWithRunContext(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	graph := &closingGraphReader{entered: make(chan struct{}, 1)}
	getenv := func(key string) string {
		if key == graphGaugeRefreshTimeoutEnv {
			return "1h"
		}
		return ""
	}
	refresher, err := registerGraphBackedGauges(&telemetry.Instruments{}, provider.Meter("test"), graph, nil, getenv, nil)
	if err != nil {
		t.Fatalf("registerGraphBackedGauges() error = %v", err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	wait := startGraphGaugeRefresher(runCtx, refresher)
	select {
	case <-graph.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("refresher never started its graph read")
	}
	cancelRun()

	stopped := make(chan struct{})
	go func() { wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh loops still running after the run context was cancelled")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, gauge := range []string{gaugeEdgesBySourceTool, gaugeFilesByLanguage} {
		for _, outcome := range []string{"error", "timeout", "success"} {
			if got := refreshOutcomes(rm, gauge, outcome); got != 0 {
				t.Fatalf("%s %s outcomes after shutdown = %d, want 0", gauge, outcome, got)
			}
		}
	}
}
