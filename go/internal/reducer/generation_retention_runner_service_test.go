// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/stretchr/testify/require"
)

// TestServiceStartsGenerationRetentionRunner proves Service.startSideRunners
// starts the maintenance.GenerationRetentionRunner goroutine. The runner
// moved to [maintenance] in #6061, but Service.Run's side-runner startup
// stays a root concern, so this wiring proof stays here; the runner-behavior
// tests that used to live beside it moved to
// go/internal/reducer/maintenance/generation_retention_runner_test.go.
func TestServiceStartsGenerationRetentionRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pruner := &fakeRootGenerationRetentionPruner{
		results: []maintenance.GenerationRetentionResult{{RowsPruned: map[string]int64{}}},
	}
	started := make(chan struct{}, 1)
	runner := &maintenance.GenerationRetentionRunner{
		Pruner: pruner,
		Config: maintenance.GenerationRetentionRunnerConfig{PollInterval: time.Hour},
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

// fakeRootGenerationRetentionPruner is a minimal single-use double for
// maintenance.GenerationRetentionPruner, scoped to this file's one wiring
// test. The full-featured fake used by the runner's own behavior tests lives
// beside them in maintenance, unexported and out of this package's reach.
type fakeRootGenerationRetentionPruner struct {
	mu      sync.Mutex
	calls   int
	results []maintenance.GenerationRetentionResult
}

func (p *fakeRootGenerationRetentionPruner) PruneSupersededGenerations(
	_ context.Context,
	_ maintenance.GenerationRetentionPolicy,
) (maintenance.GenerationRetentionResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if len(p.results) == 0 {
		return maintenance.GenerationRetentionResult{RowsPruned: map[string]int64{}}, nil
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

func (p *fakeRootGenerationRetentionPruner) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
