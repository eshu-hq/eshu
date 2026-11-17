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

// fakeRelationshipSweeper records each request and answers from results in
// order; with err set it returns partial and err, as a pass whose census
// succeeded but whose batch failed does.
type fakeRelationshipSweeper struct {
	requests []RelationshipSweepRequest
	results  []RelationshipSweepResult
	partial  RelationshipSweepResult
	err      error
}

func (s *fakeRelationshipSweeper) SweepRelationships(_ context.Context, request RelationshipSweepRequest) (RelationshipSweepResult, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return s.partial, s.err
	}
	if len(s.results) == 0 {
		return RelationshipSweepResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

// idleWaits runs the loop until its first idle wait and returns the duration it
// asked for: the pruner finds nothing, so the loop sleeps after one pass.
func idleWaits(t *testing.T, sweeper *fakeRelationshipSweeper, poll time.Duration) time.Duration {
	t.Helper()
	var waited []time.Duration
	runner := &Runner{
		Pruner:              &fakeGenerationRetentionPruner{},
		RelationshipSweeper: sweeper,
		Config:              Config{PollInterval: poll},
		Wait: func(_ context.Context, d time.Duration) error {
			waited = append(waited, d)
			return context.Canceled
		},
	}
	require.NoError(t, runner.Run(context.Background()))
	require.Len(t, waited, 1)
	return waited[0]
}

// TestRelationshipSweepAcceleratesOnlyAfterACappedPass is the cadence test of
// the #10823 ruling: the loop waits a sixth of the poll interval only after a
// plain capped pass. A clean pass, a failed pass and a capped pass that also hit
// a lock timeout keep the full poll interval, so a failing or contended sweep
// never hammers the database.
func TestRelationshipSweepAcceleratesOnlyAfterACappedPass(t *testing.T) {
	poll := 6 * time.Hour
	for _, tc := range []struct {
		name    string
		sweeper *fakeRelationshipSweeper
		want    time.Duration
	}{
		{"capped by rows", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{Capped: "rows"}}}, time.Hour},
		{"capped by deadline", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{Capped: "deadline"}}}, time.Hour},
		{"capped by the list limit", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{Capped: "list"}}}, time.Hour},
		{"clean", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{GenerationsSwept: 12}}}, poll},
		{"census only", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{OrphanGenerations: map[string]int64{"relationship_evidence_facts": 9}}}}, poll},
		{"failed", &fakeRelationshipSweeper{err: errors.New("batch failed"), partial: RelationshipSweepResult{Capped: "rows"}}, poll},
		{"capped with a lock timeout", &fakeRelationshipSweeper{results: []RelationshipSweepResult{{
			Capped: "rows", Skipped: map[string]int{"lock_timeout": 3},
		}}}, poll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, idleWaits(t, tc.sweeper, poll))
		})
	}
}

// TestRelationshipSweepIsDueAfterAcceleratedIntervalOnlyWhenCapped pins the due
// check that backs the shorter wait: a capped pass is due again a sixth of the
// interval later, every other pass only after the full interval. Without it the
// shorter wait would wake the loop and find the sweep not yet due.
func TestRelationshipSweepIsDueAfterAcceleratedIntervalOnlyWhenCapped(t *testing.T) {
	poll := 6 * time.Hour
	for _, tc := range []struct {
		name      string
		result    RelationshipSweepResult
		err       error
		dueAfter  time.Duration
		notBefore time.Duration
	}{
		{"capped", RelationshipSweepResult{Capped: "rows"}, nil, time.Hour, time.Hour - time.Second},
		{"clean", RelationshipSweepResult{}, nil, poll, poll - time.Second},
		{"failed", RelationshipSweepResult{Capped: "rows"}, errors.New("boom"), poll, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
			sweeper := &fakeRelationshipSweeper{results: []RelationshipSweepResult{tc.result}}
			if tc.err != nil {
				sweeper = &fakeRelationshipSweeper{err: tc.err, partial: tc.result}
			}
			runner := &Runner{
				Pruner: &fakeGenerationRetentionPruner{}, RelationshipSweeper: sweeper,
				Config: Config{PollInterval: poll}, Now: func() time.Time { return now },
			}
			_, err := runner.RunOnce(context.Background())
			require.NoError(t, err, "a sweep failure must not fail the retention pass")
			require.Len(t, sweeper.requests, 1)

			now = now.Add(tc.notBefore)
			_, err = runner.RunOnce(context.Background())
			require.NoError(t, err)
			require.Len(t, sweeper.requests, 1, "not due yet at %s", tc.notBefore)

			now = time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC).Add(tc.dueAfter)
			_, err = runner.RunOnce(context.Background())
			require.NoError(t, err)
			require.Len(t, sweeper.requests, 2, "due at %s", tc.dueAfter)
		})
	}
}

// TestRelationshipSweepDeletesAreOffByDefault pins the rollout: a zero policy
// asks the sweeper for the census only, the opt-in flag turns deletes on, and
// the row cap and the quarter-poll deadline reach the sweeper.
func TestRelationshipSweepDeletesAreOffByDefault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy Policy
		want   RelationshipSweepRequest
	}{
		{"default", Policy{}, RelationshipSweepRequest{Deadline: 15 * time.Minute}},
		{
			"enabled",
			Policy{RelationshipRetentionSweepEnabled: true, RelationshipRetentionRowCap: 25_000},
			RelationshipSweepRequest{Deadline: 15 * time.Minute, RowCap: 25_000, Deletes: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sweeper := &fakeRelationshipSweeper{}
			runner := &Runner{
				Pruner: &fakeGenerationRetentionPruner{}, RelationshipSweeper: sweeper,
				Config: Config{PollInterval: time.Hour, Policy: tc.policy},
			}
			_, err := runner.RunOnce(context.Background())
			require.NoError(t, err)
			require.Equal(t, []RelationshipSweepRequest{tc.want}, sweeper.requests)
		})
	}
}

// TestRelationshipSweepRecordsOperatorSignals pins the 3 AM signals: the orphan
// gauge per table (zero included), the rows swept, the skip reasons, the cap
// counter, the two phases and the pass log line; and a failure on the shared
// failure counter without failing the retention pass, the census still
// recorded.
func TestRelationshipSweepRecordsOperatorSignals(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	sweeper := &fakeRelationshipSweeper{results: []RelationshipSweepResult{{
		OrphanGenerations: map[string]int64{
			"relationship_evidence_facts": 5000, "relationship_candidates": 120, "relationship_generations": 0,
		},
		CensusSaturated:  true,
		RowsSwept:        map[string]int64{"relationship_evidence_facts": 90000, "relationship_candidates": 9000, "relationship_generations": 400},
		Skipped:          map[string]int{"lock_busy": 2, "lock_timeout": 0, "ineligible": 1},
		GenerationsSwept: 400,
		Batches:          8,
		Capped:           "rows",
		CensusDuration:   2 * time.Second,
		SweepDuration:    3 * time.Second,
	}}}
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{}, RelationshipSweeper: sweeper,
		Config:      Config{PollInterval: time.Hour, Policy: Policy{RelationshipRetentionSweepEnabled: true}},
		Instruments: instruments, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now: func() time.Time { return now },
	}
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(5000), testutil.GaugeValue(t, rm, "eshu_dp_relationship_retention_orphan_generations",
		map[string]string{"table": "relationship_evidence_facts"}))
	require.Equal(t, int64(0), testutil.GaugeValue(t, rm, "eshu_dp_relationship_retention_orphan_generations",
		map[string]string{"table": "relationship_generations"}))
	require.Equal(t, int64(90000), testutil.CounterValue(t, rm, "eshu_dp_relationship_retention_rows_swept_total",
		map[string]string{"table": "relationship_evidence_facts"}))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm, "eshu_dp_relationship_retention_generations_skipped_total",
		map[string]string{"reason": "lock_busy"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_relationship_retention_capped_total",
		map[string]string{"cap": "rows"}))
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds",
		map[string]string{"phase": "census_relationship_orphans"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 2.0, sum, 1e-9)
	count, sum = retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds",
		map[string]string{"phase": "sweep_relationship_orphans"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 3.0, sum, 1e-9)
	require.Contains(t, logs.String(), `"msg":"generation retention relationship sweep completed"`)
	require.Contains(t, logs.String(), `"deletes_enabled":true`)
	require.Contains(t, logs.String(), `"census_saturated":true`)
	require.Contains(t, logs.String(), `"capped":"rows"`)

	// A pass whose census succeeded and whose batch then failed still sets the
	// gauge and counts the committed rows; the failure is counted and logged.
	sweeper.partial = RelationshipSweepResult{
		OrphanGenerations: map[string]int64{"relationship_evidence_facts": 7},
		RowsSwept:         map[string]int64{"relationship_evidence_facts": 300},
		CensusDuration:    time.Second,
	}
	sweeper.err = errors.New("deadlock detected")
	now = now.Add(2 * time.Hour)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err, "a failed sweep must not fail the retention pass")
	rm = metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(7), testutil.GaugeValue(t, rm, "eshu_dp_relationship_retention_orphan_generations",
		map[string]string{"table": "relationship_evidence_facts"}))
	require.Equal(t, int64(90300), testutil.CounterValue(t, rm, "eshu_dp_relationship_retention_rows_swept_total",
		map[string]string{"table": "relationship_evidence_facts"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_generation_retention_failures_total",
		map[string]string{"reason": "relationship_orphan_sweep"}))
	require.Contains(t, logs.String(), `"failure_class":"generation_retention_relationship_sweep_error"`)
}
