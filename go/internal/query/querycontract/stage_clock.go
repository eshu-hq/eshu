// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"sync/atomic"
	"time"
)

// requestStagesKey carries the per-request *RequestStages through a request
// context. It is unexported so only this package installs and reads the
// clock; handlers record through ObservePG and ObserveGraph, and the
// request-metrics middleware reads the totals for the Server-Timing trailer.
type requestStagesKey struct{}

// RequestStages accumulates per-request Postgres and graph store time for
// the Server-Timing trailer (issue #7733 item 2: a handler total alone
// cannot attribute a slow search to Postgres versus the graph).
//
// Each stage sums the durations of its own store calls on the request. The
// accumulation is race-safe: parallel store calls add concurrently without
// a lock. Overlap accounting is defined as a sum, not a wall-clock union:
// two 100ms phases that run concurrently record pg=100ms and graph=100ms
// against a ~100ms total, so pg+graph may exceed the total. The trailer
// documents that reading.
//
// Durations live only in the trailer value, never in metric labels, so no
// cardinality grows with request traffic.
type RequestStages struct {
	pg    atomic.Int64
	graph atomic.Int64
}

// WithRequestStages installs a fresh per-request stage clock on ctx and
// returns both. The request-metrics middleware installs it before routing;
// a handler that runs outside that middleware (or a test that wants its
// own clock) installs it the same way.
func WithRequestStages(ctx context.Context) (context.Context, *RequestStages) {
	stages := &RequestStages{}
	return context.WithValue(ctx, requestStagesKey{}, stages), stages
}

// requestStagesOf returns the clock on ctx, or nil when the request runs
// without one. Recording without a clock is a no-op so store seams stay
// safe on paths the middleware never wraps.
func requestStagesOf(ctx context.Context) *RequestStages {
	if ctx == nil {
		return nil
	}
	stages, _ := ctx.Value(requestStagesKey{}).(*RequestStages)
	return stages
}

// ObservePG adds one Postgres store call's duration to the request's pg
// stage. It is a no-op when ctx carries no stage clock.
func ObservePG(ctx context.Context, d time.Duration) {
	if stages := requestStagesOf(ctx); stages != nil {
		stages.pg.Add(int64(d))
	}
}

// ObserveGraph adds one graph store call's duration to the request's graph
// stage. It is a no-op when ctx carries no stage clock.
func ObserveGraph(ctx context.Context, d time.Duration) {
	if stages := requestStagesOf(ctx); stages != nil {
		stages.graph.Add(int64(d))
	}
}

// PG reports the accumulated Postgres stage duration.
func (s *RequestStages) PG() time.Duration {
	if s == nil {
		return 0
	}
	return time.Duration(s.pg.Load())
}

// Graph reports the accumulated graph stage duration.
func (s *RequestStages) Graph() time.Duration {
	if s == nil {
		return 0
	}
	return time.Duration(s.graph.Load())
}
