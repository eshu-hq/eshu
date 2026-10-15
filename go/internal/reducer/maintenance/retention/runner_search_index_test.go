// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestGenerationRetentionRunnerBoundsSearchIndexPredeleteByPollInterval pins
// the #7427 deadline: an unset pre-delete deadline becomes a quarter of the
// poll interval, and an explicit one is passed through unchanged.
func TestGenerationRetentionRunnerBoundsSearchIndexPredeleteByPollInterval(t *testing.T) {
	pruner := &fakeGenerationRetentionPruner{}
	runner := &Runner{Pruner: pruner, Config: Config{PollInterval: 20 * time.Minute}}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, pruner.policies[0].SearchIndexPredeleteDeadline)
	require.Zero(t, runner.Config.Policy.SearchIndexPredeleteDeadline, "RunOnce must not rewrite the configured policy")

	runner.Config.Policy.SearchIndexPredeleteDeadline = time.Minute
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, time.Minute, pruner.policies[1].SearchIndexPredeleteDeadline)
}

// TestGenerationRetentionRunnerRecordsSearchIndexBatches pins the #7427
// batches-per-pass histogram, the pre-delete phases and rows, and the cycle
// log field an operator reads to see the pre-delete's share of a pass.
func TestGenerationRetentionRunnerRecordsSearchIndexBatches(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{results: []Result{{
			GenerationsPruned: 1,
			RowsPruned:        map[string]int64{"eshu_search_index_terms": 45000, "scope_generations": 1},
			Skipped:           map[string]int{"search_index_undrained": 2, "advisory_lock_busy": 1},
			PhaseDurations: map[string]time.Duration{
				"predelete_search_index": 2 * time.Second,
				"sweep_search_index":     time.Second,
			},
			SearchIndexBatches: 6,
		}}},
		Config:      Config{PollInterval: time.Hour},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	var batches *metricdata.HistogramDataPoint[int64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_generation_retention_search_index_batches" {
				histogram := m.Data.(metricdata.Histogram[int64])
				batches = &histogram.DataPoints[0]
			}
		}
	}
	require.NotNil(t, batches, "search index batches histogram not recorded")
	require.Equal(t, uint64(1), batches.Count)
	require.Equal(t, int64(6), batches.Sum)
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds", map[string]string{"phase": "sweep_search_index"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 1.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"search_index_batches":6`)
	require.Contains(t, logs.String(), `"search_index_undrained":2`)
}
