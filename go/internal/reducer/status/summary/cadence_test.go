// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scriptedWait records every wait the loop asks for and cancels the run
// after a fixed number of waits, so a test drives the loop pass by pass.
type scriptedWait struct {
	mu     sync.Mutex
	waits  []time.Duration
	limit  int
	cancel context.CancelFunc
	before func(n int)
}

func (w *scriptedWait) wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	w.waits = append(w.waits, d)
	n := len(w.waits)
	w.mu.Unlock()
	if w.before != nil {
		w.before(n)
	}
	if n >= w.limit {
		w.cancel()
		return ctx.Err()
	}
	return nil
}

func newTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// counterValue sums an Int64 counter or gauge's points whose attributes
// include every want pair.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, name string, want ...attribute.KeyValue) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			var points []metricdata.DataPoint[int64]
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				points = data.DataPoints
			case metricdata.Gauge[int64]:
				points = data.DataPoints
			default:
				t.Fatalf("%s has type %T", name, m.Data)
			}
			for _, point := range points {
				if hasAttributes(point.Attributes, want) {
					total += point.Value
				}
			}
		}
	}
	return total
}

// histogramCount counts the samples of a float histogram whose attributes
// include every want pair.
func histogramCount(t *testing.T, reader *sdkmetric.ManualReader, name string, want ...attribute.KeyValue) uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total uint64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				if hasAttributes(point.Attributes, want) {
					total += point.Count
				}
			}
		}
	}
	return total
}

func hasAttributes(set attribute.Set, want []attribute.KeyValue) bool {
	for _, kv := range want {
		value, ok := set.Value(kv.Key)
		if !ok || value != kv.Value {
			return false
		}
	}
	return true
}

// TestRunPacesPassesOnIntervalBoundariesAndCountsOverruns proves the loop
// never queues or overlaps passes: a pass shorter than the interval waits out
// the rest of it, and a pass longer than the interval is counted, warned, and
// pushes the next start to the following interval boundary.
func TestRunPacesPassesOnIntervalBoundariesAndCountsOverruns(t *testing.T) {
	t.Parallel()
	runner, _, statement, logs := newPassRunner(t)
	instruments, reader := newTestInstruments(t)
	runner.Instruments = instruments
	costs := []time.Duration{1 * time.Second, 6 * time.Second, 11 * time.Second, 5 * time.Second}
	statement.cost = costs[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter := &scriptedWait{limit: len(costs), cancel: cancel, before: func(n int) {
		if n < len(costs) {
			statement.mu.Lock()
			statement.cost = costs[n]
			statement.mu.Unlock()
		}
	}}
	runner.Wait = waiter.wait

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil on cancel", err)
	}

	want := []time.Duration{4 * time.Second, 4 * time.Second, 4 * time.Second, 5 * time.Second}
	if len(waiter.waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waiter.waits, want)
	}
	for i := range want {
		if waiter.waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waiter.waits, want)
		}
	}
	if statement.maxFlight != 1 {
		t.Fatalf("max concurrent passes = %d, want 1", statement.maxFlight)
	}
	if got := counterValue(t, reader, "eshu_dp_status_summary_writer_overrun_total",
		telemetry.AttrModelKey("active_work_summary")); got != 2 {
		t.Fatalf("overrun_total = %d, want 2 (the 6 s and 11 s passes)", got)
	}
	if got := strings.Count(logs.String(), `"msg":"status summary writer pass overran its interval"`); got != 2 {
		t.Fatalf("overrun warnings = %d, want 2", got)
	}
}

// TestRunKeepsLoopingAfterAFailedPass proves an error never ends the loop or
// crashes the reducer: the pass is counted and the next tick retries.
func TestRunKeepsLoopingAfterAFailedPass(t *testing.T) {
	t.Parallel()
	runner, database, statement, _ := newPassRunner(t)
	instruments, reader := newTestInstruments(t)
	runner.Instruments = instruments
	statement.err = errors.New("statement timeout")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Wait = (&scriptedWait{limit: 3, cancel: cancel, before: func(n int) {
		if n == 1 {
			statement.mu.Lock()
			statement.err = nil
			statement.mu.Unlock()
		}
	}}).wait

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if statement.callCount() != 3 {
		t.Fatalf("passes = %d, want 3", statement.callCount())
	}
	if database.committed != 2 {
		t.Fatalf("committed passes = %d, want 2", database.committed)
	}
	model := telemetry.AttrModelKey("active_work_summary")
	if got := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total", model, telemetry.AttrOutcome("error")); got != 1 {
		t.Fatalf("error passes = %d, want 1", got)
	}
	if got := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total", model, telemetry.AttrOutcome("ok")); got != 2 {
		t.Fatalf("ok passes = %d, want 2", got)
	}
	if got := histogramCount(t, reader, "eshu_dp_status_summary_writer_pass_duration_seconds", model, telemetry.AttrOutcome("ok")); got != 2 {
		t.Fatalf("ok pass durations = %d, want 2", got)
	}
}

// TestRunReportsUpWhileLoopingAndDownAfter proves the writer_up gauge is 1
// while the loop runs and 0 once it stops.
func TestRunReportsUpWhileLoopingAndDownAfter(t *testing.T) {
	t.Parallel()
	runner, _, _, _ := newPassRunner(t)
	instruments, reader := newTestInstruments(t)
	runner.Instruments = instruments
	model := telemetry.AttrModelKey("active_work_summary")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var upDuringRun int64 = -1
	runner.Wait = (&scriptedWait{limit: 1, cancel: cancel, before: func(int) {
		upDuringRun = counterValue(t, reader, "eshu_dp_status_summary_writer_up", model)
	}}).wait

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if upDuringRun != 1 {
		t.Fatalf("writer_up during the loop = %d, want 1", upDuringRun)
	}
	if got := counterValue(t, reader, "eshu_dp_status_summary_writer_up", model); got != 0 {
		t.Fatalf("writer_up after the loop = %d, want 0", got)
	}
}

// TestRunStopsMidPassOnShutdown proves a shutdown during a pass ends Run with
// nil, rolls the pass back, and does not count the cancelled pass as an
// error.
func TestRunStopsMidPassOnShutdown(t *testing.T) {
	t.Parallel()
	runner, database, statement, _ := newPassRunner(t)
	instruments, reader := newTestInstruments(t)
	runner.Instruments = instruments
	statement.block = make(chan struct{})
	statement.started = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- runner.Run(ctx) }()
	<-statement.started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil on shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after cancel")
	}
	if database.committed != 0 || database.rolledBack != 1 {
		t.Fatalf("committed/rolled back = %d/%d, want 0/1", database.committed, database.rolledBack)
	}
	if got := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total",
		telemetry.AttrOutcome("error")); got != 0 {
		t.Fatalf("a shutdown counted %d error passes", got)
	}
}

// TestRunUsesTheTenSecondDefault proves an unset interval paces passes at
// the 10 s default the ops-qa pass measurement chose.
func TestRunUsesTheTenSecondDefault(t *testing.T) {
	t.Parallel()
	runner, _, _, _ := newPassRunner(t)
	runner.Interval = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter := &scriptedWait{limit: 1, cancel: cancel}
	runner.Wait = waiter.wait

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(waiter.waits) != 1 || waiter.waits[0] != 10*time.Second-300*time.Millisecond {
		t.Fatalf("waits = %v, want [9.7s] after a 300 ms pass at the 10 s default", waiter.waits)
	}
}

// TestRunRefusesAnIntervalBelowTheFloor proves the measured 5 s floor is
// enforced: a faster cadence never starts a pass.
func TestRunRefusesAnIntervalBelowTheFloor(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	runner.Interval = 2 * time.Second

	if err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run() with a 2 s interval returned nil, want an error")
	}
	if database.begun != 0 {
		t.Fatalf("a refused runner opened %d transactions", database.begun)
	}
}

// TestRunRequiresItsCollaborators proves a runner missing its database or
// statement fails fast instead of looping on nil.
func TestRunRequiresItsCollaborators(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Runner){
		"database":  func(r *Runner) { r.DB = nil },
		"compute":   func(r *Runner) { r.Statement.Compute = nil },
		"model key": func(r *Runner) { r.Statement.ModelKey = " " },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner, _, _, _ := newPassRunner(t)
			mutate(runner)
			if err := runner.Run(context.Background()); err == nil {
				t.Fatalf("Run() without its %s returned nil", name)
			}
		})
	}
}
