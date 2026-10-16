// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeOrphanSweeper records each sweep's deadline and returns results in
// order.
type fakeOrphanSweeper struct {
	deadlines []time.Duration
	results   []OrphanSweepResult
	err       error
}

func (s *fakeOrphanSweeper) SweepSearchIndexOrphans(_ context.Context, deadline time.Duration) (OrphanSweepResult, error) {
	s.deadlines = append(s.deadlines, deadline)
	if s.err != nil {
		return OrphanSweepResult{}, s.err
	}
	if len(s.results) == 0 {
		return OrphanSweepResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

// TestGenerationRetentionRunnerSweepsOrphansOncePerPollInterval pins the
// #7427 PR2 cadence: the first pass sweeps, a pass inside the same poll
// interval (a draining backlog runs passes back to back) does not, and the
// first pass after the interval sweeps again with the pass's pre-delete
// deadline. A failed prune does not stop a due sweep.
func TestGenerationRetentionRunnerSweepsOrphansOncePerPollInterval(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	sweeper := &fakeOrphanSweeper{}
	pruner := &fakeGenerationRetentionPruner{errs: []error{nil, nil, errors.New("prune failed")}}
	runner := &Runner{
		Pruner:        pruner,
		OrphanSweeper: sweeper,
		Config:        Config{PollInterval: 20 * time.Minute},
		Now:           func() time.Time { return now },
	}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, sweeper.deadlines, 1)
	require.Equal(t, 5*time.Minute, sweeper.deadlines[0])

	now = now.Add(19 * time.Minute)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, sweeper.deadlines, 1, "a pass inside the poll interval must not sweep")

	now = now.Add(time.Minute)
	_, err = runner.RunOnce(context.Background())
	require.Error(t, err)
	require.Len(t, sweeper.deadlines, 2, "a due sweep runs even when the prune fails")
}

// TestGenerationRetentionRunnerRecordsOrphanSweep pins the operator signals:
// the orphan gauge per table (0 included), the swept-rows counter, the two
// phases, the cycle log line, and a sweep failure on the failure counter
// without failing the pass.
func TestGenerationRetentionRunnerRecordsOrphanSweep(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	sweeper := &fakeOrphanSweeper{results: []OrphanSweepResult{{
		PairsScanned:      map[string]int64{"eshu_search_index_terms": 31021, "eshu_search_index_documents": 31021},
		OrphanGenerations: map[string]int64{"eshu_search_index_terms": 2, "eshu_search_index_documents": 0},
		RowsSwept:         map[string]int64{"eshu_search_index_terms": 45000, "eshu_search_index_documents": 0},
		Batches:           6,
		CensusDuration:    2 * time.Second,
		SweepDuration:     time.Second,
	}}}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	runner := &Runner{
		Pruner:        &fakeGenerationRetentionPruner{},
		OrphanSweeper: sweeper,
		Config:        Config{PollInterval: time.Hour},
		Instruments:   instruments,
		Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:           func() time.Time { return now },
	}
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(2), testutil.GaugeValue(t, rm, "eshu_dp_search_index_orphan_generations",
		map[string]string{"table": "eshu_search_index_terms"}))
	require.Equal(t, int64(0), testutil.GaugeValue(t, rm, "eshu_dp_search_index_orphan_generations",
		map[string]string{"table": "eshu_search_index_documents"}))
	require.Equal(t, int64(45000), testutil.CounterValue(t, rm, "eshu_dp_search_index_orphan_rows_swept_total",
		map[string]string{"table": "eshu_search_index_terms"}))
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds",
		map[string]string{"phase": "census_search_index_orphans"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 2.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"msg":"generation retention search index orphan sweep completed"`)
	require.Contains(t, logs.String(), `"orphan_generations":{"eshu_search_index_documents":0,"eshu_search_index_terms":2}`)

	sweeper.err = errors.New("census failed")
	now = now.Add(time.Hour)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err, "a failed sweep must not fail the retention pass")
	rm = metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_generation_retention_failures_total",
		map[string]string{"reason": "search_index_orphan_sweep"}))
	require.Contains(t, logs.String(), `"failure_class":"generation_retention_search_index_orphan_sweep_error"`)
}
