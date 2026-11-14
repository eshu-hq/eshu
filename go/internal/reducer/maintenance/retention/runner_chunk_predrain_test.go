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

// TestGenerationRetentionRunnerBoundsChunkPredeleteByPollInterval pins the
// #10020 deadline: an unset chunk pre-delete deadline becomes a quarter of
// the poll interval, and an explicit one is passed through unchanged.
func TestGenerationRetentionRunnerBoundsChunkPredeleteByPollInterval(t *testing.T) {
	pruner := &fakeGenerationRetentionPruner{}
	runner := &Runner{Pruner: pruner, Config: Config{PollInterval: 20 * time.Minute}}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, pruner.policies[0].ChunkPredeleteDeadline)
	require.Zero(t, runner.Config.Policy.ChunkPredeleteDeadline, "RunOnce must not rewrite the configured policy")

	runner.Config.Policy.ChunkPredeleteDeadline = time.Minute
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, time.Minute, pruner.policies[1].ChunkPredeleteDeadline)
}

// TestGenerationRetentionRunnerRecordsChunkPredeleteBatches pins the #10020
// batches-per-pass histogram, the predrain phase, and the cycle log field an
// operator reads to see the chunk pre-delete's share of a pass. It also pins
// the predrained-over-limit count Gate-3 reads to tell a predrained-normal
// cycle (batches > 0, over-limit count 0) from a chunk-shaped one.
func TestGenerationRetentionRunnerRecordsChunkPredeleteBatches(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{results: []Result{{
			GenerationsPruned: 1,
			RowsPruned:        map[string]int64{"fact_records": 250000, "scope_generations": 1},
			Skipped:           map[string]int{"row_limit_chunked": 1},
			PhaseDurations: map[string]time.Duration{
				"predrain_chunk": 2 * time.Second,
			},
			ChunkPredeleteBatches:          4,
			PredrainedOverLimitGenerations: 2,
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
			if m.Name == "eshu_dp_generation_retention_chunk_predelete_batches" {
				histogram := m.Data.(metricdata.Histogram[int64])
				batches = &histogram.DataPoints[0]
			}
		}
	}
	require.NotNil(t, batches, "chunk pre-delete batches histogram not recorded")
	require.Equal(t, uint64(1), batches.Count)
	require.Equal(t, int64(4), batches.Sum)
	var overLimit *metricdata.HistogramDataPoint[int64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_generation_retention_predrained_over_limit_generations" {
				histogram := m.Data.(metricdata.Histogram[int64])
				overLimit = &histogram.DataPoints[0]
			}
		}
	}
	require.NotNil(t, overLimit, "predrained over-limit generations histogram not recorded")
	require.Equal(t, uint64(1), overLimit.Count)
	require.Equal(t, int64(2), overLimit.Sum)
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds", map[string]string{"phase": "predrain_chunk"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 2.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"chunk_predelete_batches":4`)
	require.Contains(t, logs.String(), `"predrained_over_limit_generations":2`)
}
