// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestGenerationRetentionRunnerDrainsUntilEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeGenerationRetentionPruner{
		results: []Result{
			{GenerationsPruned: 2, RowsPruned: map[string]int64{"scope_generations": 2}},
			{GenerationsPruned: 0, RowsPruned: map[string]int64{}},
		},
	}
	waitCalls := 0
	runner := &Runner{
		Pruner: pruner,
		Config: Config{
			PollInterval: time.Hour,
			Policy: Policy{
				MinSupersededGenerations: 24,
				MaxSupersededAge:         7 * 24 * time.Hour,
				BatchGenerationLimit:     100,
				BatchRowLimit:            100_000,
				PolicyScope:              "global",
				PolicyRevision:           "test-policy",
			},
		},
		Wait: func(context.Context, time.Duration) error {
			waitCalls++
			cancel()
			return context.Canceled
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, pruner.callCount())
	require.Equal(t, 1, waitCalls)
	require.Equal(t, "test-policy", pruner.policies[0].PolicyRevision)
}

// TestGenerationRetentionRunnerRetriesSoonAfterSkippedPass pins the #7398
// cadence: a pass that prunes nothing but reports skipped candidates retries
// soon instead of sleeping the full poll interval, while a truly empty pass
// keeps the full sleep.
func TestGenerationRetentionRunnerRetriesSoonAfterSkippedPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeGenerationRetentionPruner{
		results: []Result{
			{RowsPruned: map[string]int64{}, Skipped: map[string]int{"row_limit": 2}},
			{RowsPruned: map[string]int64{}},
		},
	}
	var waits []time.Duration
	runner := &Runner{
		Pruner: pruner,
		Config: Config{PollInterval: time.Hour},
		Wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 2 {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, pruner.callCount())
	require.Equal(t, []time.Duration{defaultGenerationRetentionSkippedRetryBaseInterval, time.Hour}, waits)
}

// TestGenerationRetentionRunnerSkippedRetryBacksOffToPollInterval pins the
// #7398 bound: consecutive skipped-only passes double the retry delay up to
// the poll interval, so a permanently-blocked backlog converges back to the
// configured cadence instead of retrying hot forever.
func TestGenerationRetentionRunnerSkippedRetryBacksOffToPollInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{
			RowsPruned: map[string]int64{},
			Skipped:    map[string]int{"row_limit": 1},
		}
	}
	pruner := &fakeGenerationRetentionPruner{results: results}
	var waits []time.Duration
	runner := &Runner{
		Pruner: pruner,
		Config: Config{PollInterval: time.Hour},
		Wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 8 {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	require.Equal(t, 8, pruner.callCount())
	require.Equal(t, []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour,
	}, waits)
	for _, wait := range waits {
		require.LessOrEqual(t, wait, time.Hour)
	}
}

// TestGenerationRetentionRunnerSkippedBackoffResetsAfterPrune pins the #7398
// reset: a pass that prunes clears the skipped streak, so the next
// skipped-only pass retries at the base interval again.
func TestGenerationRetentionRunnerSkippedBackoffResetsAfterPrune(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeGenerationRetentionPruner{
		results: []Result{
			{RowsPruned: map[string]int64{}, Skipped: map[string]int{"row_limit": 2}},
			{GenerationsPruned: 1, RowsPruned: map[string]int64{"scope_generations": 1}},
			{RowsPruned: map[string]int64{}, Skipped: map[string]int{"row_limit": 2}},
			{RowsPruned: map[string]int64{}},
		},
	}
	var waits []time.Duration
	runner := &Runner{
		Pruner: pruner,
		Config: Config{PollInterval: time.Hour},
		Wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 3 {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	require.Equal(t, 4, pruner.callCount())
	require.Equal(t, []time.Duration{
		defaultGenerationRetentionSkippedRetryBaseInterval,
		defaultGenerationRetentionSkippedRetryBaseInterval,
		time.Hour,
	}, waits)
}

func TestGenerationRetentionRunnerValidation(t *testing.T) {
	runner := &Runner{}

	_, err := runner.RunOnce(context.Background())

	require.ErrorContains(t, err, "generation retention pruner is required")
}

func TestGenerationRetentionRunnerRecordsSkipReasonMetric(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	require.NoError(t, err)
	pruner := &fakeGenerationRetentionPruner{
		results: []Result{{
			RowsPruned: map[string]int64{},
			Skipped:    map[string]int{"row_limit": 2},
		}},
	}
	runner := &Runner{
		Pruner:      pruner,
		Config:      Config{PollInterval: time.Hour},
		Instruments: instruments,
	}

	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(2), testutil.CounterValue(
		t,
		rm,
		"eshu_dp_generation_retention_skipped_total",
		map[string]string{"reason": "row_limit"},
	))
}

type fakeGenerationRetentionPruner struct {
	mu       sync.Mutex
	calls    int
	policies []Policy
	results  []Result
	errs     []error
}

func (p *fakeGenerationRetentionPruner) PruneSupersededGenerations(
	_ context.Context,
	policy Policy,
) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.policies = append(p.policies, policy)
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		if err != nil {
			return Result{}, err
		}
	}
	if len(p.results) == 0 {
		return Result{RowsPruned: map[string]int64{}}, nil
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

func (p *fakeGenerationRetentionPruner) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
