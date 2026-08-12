// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// concurrencyWorkers is the number of intents the concurrency test runs through
// Execute at once. The gauge assertions are exact, not lower bounds, so this is
// the value every in-flight worker must observe.
const concurrencyWorkers = 2

// concurrencyBarrierWait bounds how long a worker waits for its peers to reach
// the barrier. It only elapses when Execute stops running intents concurrently,
// which turns that regression into a failure naming the observed count instead
// of a hang until the package timeout.
const concurrencyBarrierWait = 30 * time.Second

func TestActiveWorkerExecutorTracksConcurrency(t *testing.T) {
	t.Parallel()
	active := new(atomic.Int64)
	observer := reducerWorkerObserver{active: active}

	// entered closes allEntered once every worker is inside inner.Execute.
	// sampled reports that every worker has read the counter, which is what lets
	// the test hold release open until the readings are taken.
	var entered, sampled sync.WaitGroup
	entered.Add(concurrencyWorkers)
	sampled.Add(concurrencyWorkers)
	allEntered := make(chan struct{})
	go func() {
		entered.Wait()
		close(allEntered)
	}()

	release := make(chan struct{})
	observed := make(chan int64, concurrencyWorkers)

	exec := newActiveWorkerExecutor(fakeExecutor{onExecute: func() {
		entered.Done()
		// Park until every worker is inside inner.Execute. Past this barrier no
		// Execute can have returned and decremented the counter, and none can
		// return until release closes, so the reading below is forced by
		// happens-before rather than caught at a lucky instant.
		select {
		case <-allEntered:
		case <-time.After(concurrencyBarrierWait):
		}
		observed <- active.Load()
		sampled.Done()
		<-release
	}}, active)

	var wg sync.WaitGroup
	for range concurrencyWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = exec.Execute(context.Background(), reducer.Intent{})
		}()
	}

	select {
	case <-allEntered:
		// Every execution is in flight, so the gauge must report them all.
		counts, err := observer.ActiveWorkers(context.Background())
		if err != nil {
			t.Errorf("ActiveWorkers() error = %v", err)
		} else if counts["reducer"] != concurrencyWorkers {
			t.Errorf("ActiveWorkers()[reducer] = %d, want %d", counts["reducer"], concurrencyWorkers)
		}
		// Safe to wait: every worker is past the barrier, and the buffered
		// channel means none can block publishing its reading.
		sampled.Wait()
	case <-time.After(concurrencyBarrierWait):
		t.Errorf("only %d of %d workers reached the barrier within %s: Execute is not running intents concurrently",
			active.Load(), concurrencyWorkers, concurrencyBarrierWait)
	}

	close(release)
	wg.Wait()
	close(observed)

	// Each worker read the counter while all of them were parked, so every
	// reading must be the full worker count.
	for got := range observed {
		if got != concurrencyWorkers {
			t.Errorf("worker observed %d concurrent executions, want %d", got, concurrencyWorkers)
		}
	}

	// After completion the counter returns to zero.
	if got := active.Load(); got != 0 {
		t.Errorf("active counter = %d after completion, want 0", got)
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
