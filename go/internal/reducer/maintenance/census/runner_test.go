// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package census

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeCensusSource returns scripted census results, one per pass.
type fakeCensusSource struct {
	mu      sync.Mutex
	results []censusResult
	calls   int
}

type censusResult struct {
	census anchor.Census
	err    error
}

func (f *fakeCensusSource) AnchorCensus(context.Context) (anchor.Census, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i >= len(f.results) {
		i = len(f.results) - 1
	}
	return f.results[i].census, f.results[i].err
}

func (f *fakeCensusSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type censusTelemetry struct {
	reader   *sdkmetric.ManualReader
	inst     *telemetry.Instruments
	logs     *bytes.Buffer
	logger   *slog.Logger
	collect  func(t *testing.T) metricdata.ResourceMetrics
	passes   func(t *testing.T) map[string]int64
	gauge    func(t *testing.T, name string) (int64, bool)
	durCount func(t *testing.T) uint64
}

func newCensusTelemetry(t *testing.T) *censusTelemetry {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logs := &bytes.Buffer{}
	ct := &censusTelemetry{reader: reader, inst: inst, logs: logs, logger: slog.New(slog.NewTextHandler(logs, nil))}
	ct.collect = func(t *testing.T) metricdata.ResourceMetrics {
		t.Helper()
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		return rm
	}
	ct.gauge = func(t *testing.T, name string) (int64, bool) {
		for _, scope := range ct.collect(t).ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name != name {
					continue
				}
				g, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || len(g.DataPoints) != 1 {
					t.Fatalf("%s has type %T or unexpected points", name, m.Data)
				}
				return g.DataPoints[0].Value, true
			}
		}
		return 0, false
	}
	ct.passes = func(t *testing.T) map[string]int64 {
		out := map[string]int64{}
		for _, scope := range ct.collect(t).ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name != "eshu_dp_graph_id_anchor_census_passes_total" {
					continue
				}
				for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
					for _, a := range p.Attributes.ToSlice() {
						if string(a.Key) == telemetry.MetricDimensionOutcome {
							out[a.Value.AsString()] += p.Value
						}
					}
				}
			}
		}
		return out
	}
	ct.durCount = func(t *testing.T) uint64 {
		var n uint64
		for _, scope := range ct.collect(t).ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name == "eshu_dp_graph_id_anchor_census_duration_seconds" {
					for _, p := range m.Data.(metricdata.Histogram[float64]).DataPoints {
						n += p.Count
					}
				}
			}
		}
		return n
	}
	return ct
}

const (
	unreachableGauge = "eshu_dp_graph_id_anchor_unreachable_nodes"
	idBearingGauge   = "eshu_dp_graph_id_anchor_id_bearing_nodes"
	lastSuccessGauge = "eshu_dp_graph_id_anchor_census_last_success_unixtime"
)

func TestIDAnchorCensusPassRecordsGaugeAndStartupLine(t *testing.T) {
	ct := newCensusTelemetry(t)
	source := &fakeCensusSource{results: []censusResult{{census: anchor.Census{IDBearing: 898874, ViaID: 7911, ViaUIDOnly: 890963}}}}
	runner := Runner{Source: source, Instruments: ct.inst, Logger: ct.logger, Timeout: time.Second}

	runner.RunOnce(context.Background(), true)

	if v, ok := ct.gauge(t, unreachableGauge); !ok || v != 0 {
		t.Fatalf("unreachable gauge = %d (present %t), want 0", v, ok)
	}
	if v, ok := ct.gauge(t, idBearingGauge); !ok || v != 898874 {
		t.Fatalf("id-bearing gauge = %d (present %t), want the pass's 898874", v, ok)
	}
	if v, ok := ct.gauge(t, lastSuccessGauge); !ok || v <= 0 {
		t.Fatalf("last-success gauge = %d (present %t), want a unix time", v, ok)
	}
	if got := ct.passes(t); got["ok"] != 1 || len(got) != 1 {
		t.Fatalf("passes = %v, want ok=1", got)
	}
	if ct.durCount(t) != 1 {
		t.Fatalf("duration observations = %d, want 1", ct.durCount(t))
	}
	line := ct.logs.String()
	for _, want := range []string{"id anchor census", "snapshot=true", "first_pass=true", "unreachable_nodes=0", "id_bearing_nodes=898874", "level=INFO"} {
		if !strings.Contains(line, want) {
			t.Errorf("startup line lacks %q:\n%s", want, line)
		}
	}
}

// An empty graph reads zero unreachable nodes too, so the rollout gate
// "unreachable_nodes = 0 AND id_bearing_nodes > 0" must be readable from the
// metrics alone: the id-bearing gauge is recorded in the same pass and reads
// zero for an empty graph, where a healthy graph reads its id-bearing count.
func TestIDAnchorCensusEmptyGraphReadsZeroIDBearingBesideZeroUnreachable(t *testing.T) {
	ct := newCensusTelemetry(t)
	source := &fakeCensusSource{results: []censusResult{{census: anchor.Census{}}}}
	runner := Runner{Source: source, Instruments: ct.inst, Logger: ct.logger, Timeout: time.Second}

	runner.RunOnce(context.Background(), true)

	if v, ok := ct.gauge(t, unreachableGauge); !ok || v != 0 {
		t.Fatalf("unreachable gauge = %d (present %t), want 0", v, ok)
	}
	if v, ok := ct.gauge(t, idBearingGauge); !ok || v != 0 {
		t.Fatalf("id-bearing gauge = %d (present %t), want a recorded 0 for an empty graph", v, ok)
	}
}

// A residual is the alarm: the gauge carries the count and the line is a
// warning, so an operator sees it without reading the gauge.
func TestIDAnchorCensusResidualWarnsAndSetsGauge(t *testing.T) {
	ct := newCensusTelemetry(t)
	source := &fakeCensusSource{results: []censusResult{{census: anchor.Census{IDBearing: 10, ViaUIDOnly: 7, Residual: 3}}}}
	runner := Runner{Source: source, Instruments: ct.inst, Logger: ct.logger, Timeout: time.Second}

	runner.RunOnce(context.Background(), false)

	if v, _ := ct.gauge(t, unreachableGauge); v != 3 {
		t.Fatalf("unreachable gauge = %d, want 3", v)
	}
	if v, _ := ct.gauge(t, idBearingGauge); v != 10 {
		t.Fatalf("id-bearing gauge = %d, want 10", v)
	}
	if line := ct.logs.String(); !strings.Contains(line, "level=WARN") || !strings.Contains(line, "unreachable_nodes=3") {
		t.Fatalf("a residual must log at WARN with the count:\n%s", line)
	}
}

// A failed pass must not move the gauge or the last-success time: the gauge
// keeps its last good snapshot and the age of that snapshot grows, which is the
// honest signal.
func TestIDAnchorCensusFailedPassKeepsLastGoodSnapshot(t *testing.T) {
	ct := newCensusTelemetry(t)
	source := &fakeCensusSource{results: []censusResult{
		{census: anchor.Census{IDBearing: 5, ViaUIDOnly: 5}},
		{err: errors.New("bolt down")},
	}}
	clock := time.Unix(1_800_000_000, 0)
	runner := Runner{
		Source: source, Instruments: ct.inst, Logger: ct.logger, Timeout: time.Second,
		Now: func() time.Time { return clock },
	}
	runner.RunOnce(context.Background(), true)
	firstSuccess, _ := ct.gauge(t, lastSuccessGauge)
	clock = clock.Add(time.Hour)
	runner.RunOnce(context.Background(), false)

	if v, _ := ct.gauge(t, unreachableGauge); v != 0 {
		t.Fatalf("unreachable gauge = %d after a failed pass, want the last good 0", v)
	}
	if v, _ := ct.gauge(t, idBearingGauge); v != 5 {
		t.Fatalf("id-bearing gauge = %d after a failed pass, want the last good 5", v)
	}
	if v, _ := ct.gauge(t, lastSuccessGauge); v != firstSuccess {
		t.Fatalf("last-success moved from %d to %d on a failed pass", firstSuccess, v)
	}
	if got := ct.passes(t); got["ok"] != 1 || got["failed"] != 1 {
		t.Fatalf("passes = %v, want ok=1 failed=1", got)
	}
	if !strings.Contains(ct.logs.String(), "level=ERROR") {
		t.Fatalf("a failed pass must log at ERROR:\n%s", ct.logs.String())
	}
}

// A graph that never answers must not wedge the loop: the pass runs under its
// own deadline.
func TestIDAnchorCensusPassIsBoundedByTimeout(t *testing.T) {
	ct := newCensusTelemetry(t)
	hang := hangingCensusSource{}
	runner := Runner{Source: hang, Instruments: ct.inst, Logger: ct.logger, Timeout: 20 * time.Millisecond}
	done := make(chan struct{})
	go func() { runner.RunOnce(context.Background(), true); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a hanging census outlived its timeout")
	}
	if got := ct.passes(t); got["failed"] != 1 {
		t.Fatalf("passes = %v, want failed=1", got)
	}
}

type hangingCensusSource struct{}

func (hangingCensusSource) AnchorCensus(ctx context.Context) (anchor.Census, error) {
	<-ctx.Done()
	return anchor.Census{}, ctx.Err()
}

// The loop runs a first pass immediately, then one per interval, and stops on
// cancel without another pass.
func TestIDAnchorCensusLoopRunsFirstPassAtOnceThenPerInterval(t *testing.T) {
	ct := newCensusTelemetry(t)
	source := &fakeCensusSource{results: []censusResult{{census: anchor.Census{IDBearing: 1, ViaID: 1}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits []time.Duration
	runner := Runner{
		Source: source, Instruments: ct.inst, Logger: ct.logger, Timeout: time.Second, Interval: time.Hour,
		Wait: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 2 {
				cancel()
			}
			return ctx.Err()
		},
	}
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if source.callCount() != 2 {
		t.Fatalf("census ran %d times, want 2 (first pass, then one interval)", source.callCount())
	}
	if len(waits) < 1 || waits[0] != time.Hour {
		t.Fatalf("waits = %v, want the configured interval", waits)
	}
	if !strings.Contains(ct.logs.String(), "first_pass=true") || strings.Count(ct.logs.String(), "first_pass=true") != 1 {
		t.Fatalf("exactly the first pass is the startup pass:\n%s", ct.logs.String())
	}
}
