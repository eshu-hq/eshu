// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestGenerationRetentionRunnerLabelsKeyIndexRefusal pins the operator signal
// for a refused cycle (#7279): the failure counter carries the bounded reason
// key_index_unavailable instead of the generic store_error, and the log names
// the failure class an operator searches for.
func TestGenerationRetentionRunnerLabelsKeyIndexRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{errs: []error{
			fmt.Errorf("%w: fact_records_file_key_idx missing", ErrKeyIndexUnavailable),
		}},
		Config:      Config{PollInterval: time.Hour},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
		Wait: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}

	require.NoError(t, runner.Run(ctx))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_generation_retention_failures_total", map[string]string{"reason": "key_index_unavailable"}))
	require.Contains(t, logs.String(), `"failure_class":"generation_retention_key_index_unavailable"`)
	require.Contains(t, logs.String(), "fact_records_file_key_idx")
}

// TestGenerationRetentionRunnerRecordsPhasesAndScopeLockHold pins the per-phase
// durations and the scope-lock hold the store reports: each phase lands in the
// phase histogram under its bounded name, the hold in its own histogram, and
// both in the cycle log.
func TestGenerationRetentionRunnerRecordsPhasesAndScopeLockHold(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{results: []Result{{
			GenerationsPruned: 3,
			RowsPruned:        map[string]int64{"scope_generations": 3},
			PhaseDurations: map[string]time.Duration{
				"count_rows":             2 * time.Second,
				"prune_content_entities": 500 * time.Millisecond,
			},
			ScopeLockHold: 3 * time.Second,
		}}},
		Config:      Config{PollInterval: time.Hour},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds", map[string]string{"phase": "count_rows"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 2.0, sum, 1e-9)
	count, sum = retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_scope_lock_hold_seconds", map[string]string{})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 3.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"scope_lock_hold_seconds":3`)
	require.Contains(t, logs.String(), `"count_rows":2`)
}

// retentionHistogramPoint returns the count and sum of the float histogram
// data point with exactly wantAttrs.
func retentionHistogramPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, wantAttrs map[string]string) (uint64, float64) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", name, m.Data)
			}
			for _, dp := range histogram.DataPoints {
				if testutil.HasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Count, dp.Sum
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %v not found", name, wantAttrs)
	return 0, 0
}
