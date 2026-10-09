// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infra

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type reconcileCall struct {
	cursor   string
	budget   int
	suspects []string
	persist  bool
}

type fakeInfraInventoryReconciler struct {
	batches []Batch
	errs    []error
	calls   []reconcileCall
	// cancelOnCall simulates a shutdown that lands mid-cycle.
	cancelOnCall func()
}

func (f *fakeInfraInventoryReconciler) ReconcileInfraInventory(
	ctx context.Context, req Request,
) (Batch, error) {
	i := len(f.calls)
	f.calls = append(f.calls, reconcileCall{
		cursor: req.Cursor, budget: req.Budget, suspects: req.Suspects, persist: req.Persist,
	})
	if f.cancelOnCall != nil {
		f.cancelOnCall()
		return Batch{}, ctx.Err()
	}
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if i < len(f.batches) {
		return f.batches[i], err
	}
	return Batch{}, err
}

func newReconcileTestInstruments(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	return instruments, reader
}

func TestInfraInventoryReconcileRunnerCountsOutcomesAndAdvancesTheCursor(t *testing.T) {
	instruments, reader := newReconcileTestInstruments(t)
	var logs bytes.Buffer
	spans := tracetest.NewSpanRecorder()
	reconciler := &fakeInfraInventoryReconciler{batches: []Batch{
		{Ready: true, NextCursor: "repo-b", Repos: []Repo{
			{RepoID: "repo-a", Outcome: "match"},
			{RepoID: "repo-b", Outcome: "suspect", ContentRows: 3, TableRows: 2},
		}},
		{Ready: true, Repos: []Repo{
			{RepoID: "repo-b", Outcome: "repaired", ContentRows: 3, TableRows: 2},
			{RepoID: "repo-c", Outcome: "error", Err: errors.New("boom")},
		}},
		{Ready: true, Repos: []Repo{
			{RepoID: "repo-d", Outcome: "fenced", ContentRows: 1, TableRows: 0},
		}},
	}}
	runner := &Runner{
		Reconciler:  reconciler,
		Config:      Config{PollInterval: time.Minute, RepoBudget: 2},
		Tracer:      sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	for range 3 {
		_, err := runner.RunOnce(context.Background())
		require.NoError(t, err)
	}

	require.Equal(t, []reconcileCall{
		{cursor: "", budget: 2, persist: true},
		{cursor: "", budget: 2, suspects: []string{"repo-b"}, persist: true},
		{cursor: "", budget: 2, persist: true},
	}, reconciler.calls,
		"every cycle persists the walk position; a suspect is re-checked on the next cycle; "+
			"the page comes from the persisted cursor, never an in-memory one")
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for outcome, want := range map[string]int64{"match": 1, "suspect": 1, "repaired": 1, "fenced": 1, "error": 1} {
		require.Equal(t, want, testutil.CounterValue(t, rm, "eshu_dp_infra_inventory_reconcile_total",
			map[string]string{"outcome": outcome}), outcome)
	}
	require.Equal(t, uint64(3), histogramCount(t, rm, "eshu_dp_infra_inventory_reconcile_duration_seconds"))
	require.Len(t, spans.Ended(), 3)
	require.Equal(t, telemetry.SpanReducerInfraInventoryReconcile, spans.Ended()[0].Name())
	require.Contains(t, spans.Ended()[2].Attributes(), attribute.Int("eshu.infra_inventory.repos_fenced", 1),
		"a fence repair is visible on the cycle span apart from drift repairs")
	require.Contains(t, logs.String(), `"event_name":"infra_inventory.reconcile.fenced"`,
		"a fence repair logs its own event so operators can tell an unaware writer from drift")
}

func TestInfraInventoryReconcileRunnerSkipsUntilTheBackfillMarkerExists(t *testing.T) {
	instruments, reader := newReconcileTestInstruments(t)
	reconciler := &fakeInfraInventoryReconciler{batches: []Batch{{Ready: false}}}
	runner := &Runner{
		Reconciler:  reconciler,
		Config:      Config{PollInterval: time.Minute, RepoBudget: 10},
		Instruments: instruments,
	}

	batch, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.False(t, batch.Ready)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Zero(t, histogramCount(t, rm, "eshu_dp_infra_inventory_reconcile_duration_seconds"),
		"a skipped cycle is not a reconcile cycle")
}

func TestInfraInventoryReconcileRunnerRunWaitsBetweenCyclesAndCountsBatchFailure(t *testing.T) {
	instruments, reader := newReconcileTestInstruments(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reconciler := &fakeInfraInventoryReconciler{errs: []error{errors.New("list failed")}}
	var waits []time.Duration
	runner := &Runner{
		Reconciler:  reconciler,
		Config:      Config{PollInterval: 7 * time.Minute, RepoBudget: 10},
		Instruments: instruments,
		Wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			cancel()
			return context.Canceled
		},
	}

	require.NoError(t, runner.Run(ctx))
	require.Equal(t, []time.Duration{7 * time.Minute}, waits)
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_infra_inventory_reconcile_total",
		map[string]string{"outcome": "error"}))
	require.Equal(t, uint64(1), histogramCount(t, rm, "eshu_dp_infra_inventory_reconcile_duration_seconds"),
		"a failed cycle still records its duration")
}

func TestInfraInventoryReconcileRunnerShutdownMidCycleIsNotAnError(t *testing.T) {
	instruments, reader := newReconcileTestInstruments(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reconciler := &fakeInfraInventoryReconciler{cancelOnCall: cancel}
	runner := &Runner{
		Reconciler:  reconciler,
		Config:      Config{PollInterval: time.Minute, RepoBudget: 10},
		Instruments: instruments,
		Wait: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	require.NoError(t, runner.Run(ctx))
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.False(t, hasCounter(rm, "eshu_dp_infra_inventory_reconcile_total"),
		"a clean shutdown must not count an error outcome")
}

func hasCounter(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) > 0 {
				return true
			}
		}
	}
	return false
}

func TestInfraInventoryReconcileRunnerDefaultsAndValidation(t *testing.T) {
	_, err := (&Runner{}).RunOnce(context.Background())
	require.ErrorContains(t, err, "infra inventory reconciler is required")

	cfg := Config{}
	require.Equal(t, defaultInfraInventoryReconcilePollInterval, cfg.pollInterval())
	require.Equal(t, defaultInfraInventoryReconcileRepoBudget, cfg.repoBudget())
}

func histogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "metric %s data = %T", name, m.Data)
			var total uint64
			for _, dp := range hist.DataPoints {
				total += dp.Count
			}
			return total
		}
	}
	return 0
}
