// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type fakeExecutor struct {
	onExecute func()
}

func (f fakeExecutor) Execute(context.Context, reducer.Intent) (reducer.Result, error) {
	if f.onExecute != nil {
		f.onExecute()
	}
	return reducer.Result{}, nil
}

func TestNewActiveWorkerExecutorNilCounterReturnsInner(t *testing.T) {
	t.Parallel()
	got := newActiveWorkerExecutor(fakeExecutor{}, nil)
	if _, wrapped := got.(*activeWorkerExecutor); wrapped {
		t.Fatal("nil counter should return the inner executor unwrapped")
	}
}

func TestActiveWorkerExecutorTracksConcurrency(t *testing.T) {
	t.Parallel()
	active := new(atomic.Int64)
	observer := reducerWorkerObserver{active: active}

	var peak atomic.Int64
	release := make(chan struct{})
	var entered sync.WaitGroup
	entered.Add(2)

	exec := newActiveWorkerExecutor(fakeExecutor{onExecute: func() {
		if v := active.Load(); v > peak.Load() {
			peak.Store(v)
		}
		entered.Done()
		<-release
	}}, active)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = exec.Execute(context.Background(), reducer.Intent{})
		}()
	}

	entered.Wait()
	// Both executions are in flight: the gauge must report 2 active workers.
	counts, err := observer.ActiveWorkers(context.Background())
	if err != nil {
		t.Fatalf("ActiveWorkers() error = %v", err)
	}
	if counts["reducer"] != 2 {
		t.Fatalf("ActiveWorkers()[reducer] = %d, want 2", counts["reducer"])
	}
	close(release)
	wg.Wait()

	// After completion the counter returns to zero.
	if got := active.Load(); got != 0 {
		t.Fatalf("active counter = %d after completion, want 0", got)
	}
	if peak.Load() < 2 {
		t.Fatalf("peak concurrency = %d, want >= 2", peak.Load())
	}
}

func TestReducerWorkerObserverClampsNegative(t *testing.T) {
	t.Parallel()
	active := new(atomic.Int64)
	active.Store(-5)
	counts, err := reducerWorkerObserver{active: active}.ActiveWorkers(context.Background())
	if err != nil {
		t.Fatalf("ActiveWorkers() error = %v", err)
	}
	if counts["reducer"] != 0 {
		t.Fatalf("ActiveWorkers()[reducer] = %d, want 0 (clamped)", counts["reducer"])
	}
}
