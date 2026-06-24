// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// activeWorkerExecutor decorates a reducer.Executor and tracks how many intents
// are executing concurrently. Because every reducer execution path (sequential,
// per-item concurrent, and batch concurrent) runs an intent through
// Executor.Execute exactly once, the count of in-flight Execute calls equals the
// number of active reducer workers. This powers the eshu_dp_worker_pool_active
// observable gauge without touching the worker loops themselves.
//
// The counter is a single atomic, so the two increments per executed intent add
// negligible cost and introduce no lock or ordering hazard.
type activeWorkerExecutor struct {
	inner  reducer.Executor
	active *atomic.Int64
}

// newActiveWorkerExecutor wraps inner so concurrent executions are counted into
// active. A nil active counter returns inner unchanged so callers can opt out.
func newActiveWorkerExecutor(inner reducer.Executor, active *atomic.Int64) reducer.Executor {
	if active == nil {
		return inner
	}
	return &activeWorkerExecutor{inner: inner, active: active}
}

// Execute increments the active-worker counter for the duration of the inner
// execution.
func (e *activeWorkerExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	e.active.Add(1)
	defer e.active.Add(-1)
	return e.inner.Execute(ctx, intent)
}

// reducerWorkerObserver reports the current active reducer worker count for the
// eshu_dp_worker_pool_active gauge. It satisfies telemetry.WorkerObserver.
type reducerWorkerObserver struct {
	active *atomic.Int64
}

// ActiveWorkers returns the active count keyed by the "reducer" pool name. The
// value is clamped to be non-negative so a transient race during shutdown can
// never publish a negative gauge.
func (o reducerWorkerObserver) ActiveWorkers(context.Context) (map[string]int64, error) {
	n := o.active.Load()
	if n < 0 {
		n = 0
	}
	return map[string]int64{"reducer": n}, nil
}
