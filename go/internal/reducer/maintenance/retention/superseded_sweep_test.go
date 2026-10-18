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

// fakeSupersededSweeper records each sweep's deadline and returns results in
// order; with err set it returns partial and err, as a sweep whose listing
// succeeded but whose batch failed does.
type fakeSupersededSweeper struct {
	deadlines []time.Duration
	rowCaps   []int64
	results   []SupersededSweepResult
	partial   SupersededSweepResult
	err       error
}

func (s *fakeSupersededSweeper) SweepSearchIndexSuperseded(_ context.Context, deadline time.Duration, rowCap int64) (SupersededSweepResult, error) {
	s.deadlines = append(s.deadlines, deadline)
	s.rowCaps = append(s.rowCaps, rowCap)
	if s.err != nil {
		return s.partial, s.err
	}
	if len(s.results) == 0 {
		return SupersededSweepResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

// TestGenerationRetentionRunnerSupersededSweepStaysOffByDefault pins the
// #8231 enablement posture: with the flag unset the runner never calls the
// sweeper, on any pass, even when one is wired.
func TestGenerationRetentionRunnerSupersededSweepStaysOffByDefault(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	sweeper := &fakeSupersededSweeper{}
	runner := &Runner{
		Pruner:            &fakeGenerationRetentionPruner{},
		SupersededSweeper: sweeper,
		Config:            Config{PollInterval: time.Hour},
		Now:               func() time.Time { return now },
	}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, sweeper.deadlines, "an off-by-default sweep must issue no SQL without the flag")
}

// TestGenerationRetentionRunnerSweepsSupersededOncePerPollInterval pins the
// #8231 cadence: the first enabled pass sweeps, a pass inside the same poll
// interval does not, and the first pass after the interval sweeps again with
// the pass's pre-delete deadline. A failed prune does not stop a due sweep.
func TestGenerationRetentionRunnerSweepsSupersededOncePerPollInterval(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	sweeper := &fakeSupersededSweeper{}
	pruner := &fakeGenerationRetentionPruner{errs: []error{nil, nil, errors.New("prune failed")}}
	runner := &Runner{
		Pruner:            pruner,
		SupersededSweeper: sweeper,
		Config: Config{
			PollInterval: 20 * time.Minute,
			Policy:       Policy{SearchIndexSupersededSweepEnabled: true},
		},
		Now: func() time.Time { return now },
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

// TestGenerationRetentionRunnerRecordsSupersededSweep pins the operator
// signals: the swept-rows counter per table (terms, documents, stats), the
// two phases, the cycle log line, and a sweep failure on the failure counter
// without failing the pass.
func TestGenerationRetentionRunnerRecordsSupersededSweep(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	sweeper := &fakeSupersededSweeper{results: []SupersededSweepResult{{
		Candidates:    3,
		RowsSwept:     map[string]int64{"eshu_search_index_terms": 45000, "eshu_search_index_documents": 900, "eshu_search_index_stats": 3},
		Batches:       6,
		ListDuration:  2 * time.Second,
		SweepDuration: time.Second,
	}}}
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	runner := &Runner{
		Pruner:            &fakeGenerationRetentionPruner{},
		SupersededSweeper: sweeper,
		Config: Config{
			PollInterval: time.Hour,
			Policy:       Policy{SearchIndexSupersededSweepEnabled: true},
		},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:         func() time.Time { return now },
	}
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(45000), testutil.CounterValue(t, rm, "eshu_dp_search_index_superseded_rows_swept_total",
		map[string]string{"table": "eshu_search_index_terms"}))
	require.Equal(t, int64(3), testutil.CounterValue(t, rm, "eshu_dp_search_index_superseded_rows_swept_total",
		map[string]string{"table": "eshu_search_index_stats"}))
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds",
		map[string]string{"phase": "sweep_search_index_superseded"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 1.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"msg":"generation retention search index superseded sweep completed"`)
	require.Contains(t, logs.String(), `"candidates":3`)

	sweeper.err = errors.New("listing failed")
	now = now.Add(time.Hour)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err, "a failed sweep must not fail the retention pass")
	rm = metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_generation_retention_failures_total",
		map[string]string{"reason": "search_index_superseded_sweep"}))
	require.Contains(t, logs.String(), `"failure_class":"generation_retention_search_index_superseded_sweep_error"`)
}
