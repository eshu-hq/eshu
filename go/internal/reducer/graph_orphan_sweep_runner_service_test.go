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
)

// TestServiceStartsGraphOrphanSweepRunner proves Service.startSideRunners
// starts the maintenance.GraphOrphanSweepRunner goroutine. The runner moved
// to [maintenance] in #6061, but Service.Run's side-runner startup stays a
// root concern, so this wiring proof stays here; the runner-behavior tests
// that used to live beside it moved to
// go/internal/reducer/maintenance/graph_orphan_sweep_runner_test.go.
func TestServiceStartsGraphOrphanSweepRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper := &fakeRootGraphOrphanSweeper{
		results: []maintenance.GraphOrphanSweepResult{{Deleted: map[string]int64{}}},
	}
	started := make(chan struct{}, 1)
	runner := &maintenance.GraphOrphanSweepRunner{
		Sweeper: sweeper,
		Config:  maintenance.GraphOrphanSweepRunnerConfig{PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{GraphOrphanSweepRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	deadline := time.After(time.Second)
	for sweeper.callCount() != 1 {
		select {
		case <-deadline:
			t.Fatal("sweeper was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	<-started
	cancel()
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("side runner error = %v, want nil", gotErr)
	}
}

// fakeRootGraphOrphanSweeper is a minimal single-use double for
// maintenance.GraphOrphanSweeper, scoped to this file's one wiring test. The
// full-featured fake used by the runner's own behavior tests lives beside
// them in maintenance, unexported and out of this package's reach.
type fakeRootGraphOrphanSweeper struct {
	mu      sync.Mutex
	calls   int
	results []maintenance.GraphOrphanSweepResult
}

func (s *fakeRootGraphOrphanSweeper) SweepOrphanNodes(
	_ context.Context,
	_ maintenance.GraphOrphanSweepPolicy,
) (maintenance.GraphOrphanSweepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.results) == 0 {
		return maintenance.GraphOrphanSweepResult{Deleted: map[string]int64{}}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func (s *fakeRootGraphOrphanSweeper) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
