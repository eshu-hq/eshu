// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestGenerationRetentionRunnerDrainsUntilEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeGenerationRetentionPruner{
		results: []GenerationRetentionResult{
			{GenerationsPruned: 2, RowsPruned: map[string]int64{"scope_generations": 2}},
			{GenerationsPruned: 0, RowsPruned: map[string]int64{}},
		},
	}
	waitCalls := 0
	runner := &GenerationRetentionRunner{
		Pruner: pruner,
		Config: GenerationRetentionRunnerConfig{
			PollInterval: time.Hour,
			Policy: GenerationRetentionPolicy{
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

func TestGenerationRetentionRunnerValidation(t *testing.T) {
	runner := &GenerationRetentionRunner{}

	_, err := runner.RunOnce(context.Background())

	require.ErrorContains(t, err, "generation retention pruner is required")
}

func TestGenerationRetentionRunnerRecordsSkipReasonMetric(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	require.NoError(t, err)
	pruner := &fakeGenerationRetentionPruner{
		results: []GenerationRetentionResult{{
			RowsPruned: map[string]int64{},
			Skipped:    map[string]int{"row_limit": 2},
		}},
	}
	runner := &GenerationRetentionRunner{
		Pruner:      pruner,
		Config:      GenerationRetentionRunnerConfig{PollInterval: time.Hour},
		Instruments: instruments,
	}

	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(2), reducerCounterValue(
		t,
		rm,
		"eshu_dp_generation_retention_skipped_total",
		map[string]string{"reason": "row_limit"},
	))
}

func TestServiceStartsGenerationRetentionRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeGenerationRetentionPruner{
		results: []GenerationRetentionResult{{RowsPruned: map[string]int64{}}},
	}
	started := make(chan struct{}, 1)
	runner := &GenerationRetentionRunner{
		Pruner: pruner,
		Config: GenerationRetentionRunnerConfig{PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{GenerationRetentionRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	require.Eventually(t, func() bool {
		return pruner.callCount() == 1
	}, time.Second, 10*time.Millisecond)
	<-started
	cancel()
	wg.Wait()

	require.NoError(t, gotErr)
}

type fakeGenerationRetentionPruner struct {
	mu       sync.Mutex
	calls    int
	policies []GenerationRetentionPolicy
	results  []GenerationRetentionResult
	errs     []error
}

func (p *fakeGenerationRetentionPruner) PruneSupersededGenerations(
	_ context.Context,
	policy GenerationRetentionPolicy,
) (GenerationRetentionResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.policies = append(p.policies, policy)
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		if err != nil {
			return GenerationRetentionResult{}, err
		}
	}
	if len(p.results) == 0 {
		return GenerationRetentionResult{RowsPruned: map[string]int64{}}, nil
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
