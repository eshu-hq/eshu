// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// errInnerNoExecuteGroup is returned by ExecuteGroup when the wrapped executor
// does not implement GroupExecutor, mirroring TimeoutExecutor's grouped-write
// guard so the backpressure wrapper fails the same way rather than silently
// degrading a grouped write to per-statement execution.
var errInnerNoExecuteGroup = errors.New("inner executor does not support ExecuteGroup")

// BackpressureObserver receives backpressure signals from a BackpressureExecutor
// so a runtime can surface them as operator metrics without coupling this
// package to a concrete meter. waited reports that a write blocked for a permit
// (backpressure engaged); wait is how long it blocked. The interface is defined
// here because this package is the only producer; the cmd layer that wires the
// graph backend implements it against telemetry.Instruments.
type BackpressureObserver interface {
	// ObserveBackpressureWait is called once per write that had to wait for an
	// in-flight permit. wait is zero only when the permit was free immediately,
	// in which case the executor does not call this method at all.
	ObserveBackpressureWait(ctx context.Context, operation string, wait time.Duration)
}

// BackpressureExecutor bounds the number of concurrent graph writes flowing
// through Inner to MaxInFlight permits. It is the root-cause control for issue
// #3560: NornicDB write/retract timeouts recur when every reducer/projector
// worker drives a graph write at once, so a slow backend is hit by N
// simultaneous writes that all exceed their deadline and dead-letter recoverable
// work. By capping in-flight writes, a slow backend holds its permits longer,
// which blocks additional workers at the write boundary and slows intake
// (closed-loop backpressure) instead of converting transient slowness into a
// dead-letter flood.
//
// This is deliberately not a serialization fix: MaxInFlight is a configurable
// ceiling greater than one sized to backend headroom, so useful write
// concurrency is preserved and only the surplus that would overload the backend
// is gated. A non-positive MaxInFlight disables backpressure entirely, mirroring
// TimeoutExecutor's zero-timeout passthrough, so the wrapper is a safe no-op
// until an operator opts in.
//
// BackpressureExecutor should wrap the outermost retry/timeout layer so a single
// permit covers the whole write attempt (all retries and the deadline). A slow
// write therefore keeps holding its permit across retries, which is what makes
// the backpressure closed-loop.
type BackpressureExecutor struct {
	inner Executor
	gate  *BackpressureGate
}

// BackpressureGate is the shared in-flight permit pool behind one or more
// backpressure wrappers. Extracting the semaphore from BackpressureExecutor lets
// the single-statement Executor path, the grouped-write path, and the
// reducer.CypherExecutor materializer path all draw from ONE pool, so the
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT ceiling bounds every reducer graph write rather
// than per-wrapper sub-pools that would each admit maxInFlight writers (#3652).
//
// A non-positive maxInFlight leaves permits nil, which makes Acquire a no-op so a
// disabled gate adds no bound. The zero value is therefore a safe passthrough.
type BackpressureGate struct {
	permits     chan struct{}
	observer    BackpressureObserver
	inFlight    atomic.Int64
	maxInFlight int
}

// NewBackpressureGate returns a permit pool bounded to maxInFlight concurrent
// holders. A non-positive maxInFlight returns a passthrough gate that imposes no
// bound. observer is optional and, when set, is notified each time an acquire
// blocks waiting for a permit. Share one gate across every wrapper that must draw
// from the same pool.
func NewBackpressureGate(maxInFlight int, observer BackpressureObserver) *BackpressureGate {
	g := &BackpressureGate{
		observer:    observer,
		maxInFlight: maxInFlight,
	}
	if maxInFlight > 0 {
		g.permits = make(chan struct{}, maxInFlight)
	}
	return g
}

// MaxInFlight reports the configured concurrent-write ceiling. A non-positive
// value means the gate is a passthrough.
func (g *BackpressureGate) MaxInFlight() int {
	if g == nil {
		return 0
	}
	return g.maxInFlight
}

// InFlight reports the number of writes currently holding a permit so a runtime
// can register an observable gauge an operator reads at 3 AM to see how close the
// write path is to its ceiling.
func (g *BackpressureGate) InFlight() int64 {
	if g == nil {
		return 0
	}
	return g.inFlight.Load()
}

// Acquire takes one permit, blocking until a permit is free or ctx is done. It
// returns a release function the caller must defer; release is always safe to
// call (it is a no-op when the gate is disabled). When the permit is not
// immediately available, the wait duration is reported to the observer so an
// operator can see backpressure engage. operation labels the wait metric.
func (g *BackpressureGate) Acquire(ctx context.Context, operation string) (func(), error) {
	if g == nil || g.permits == nil {
		return func() {}, nil
	}

	// Fast path: a free permit with no contention is not backpressure and is not
	// reported, keeping the engaged signal meaningful.
	select {
	case g.permits <- struct{}{}:
		g.inFlight.Add(1)
		return g.releaseFunc(), nil
	default:
	}

	start := time.Now()
	select {
	case g.permits <- struct{}{}:
		g.inFlight.Add(1)
		if g.observer != nil {
			g.observer.ObserveBackpressureWait(ctx, operation, time.Since(start))
		}
		return g.releaseFunc(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// releaseFunc returns a one-shot release that returns the permit and decrements
// the in-flight gauge. It is one-shot via the deferred call site, not via
// internal guarding, because each Acquire pairs with exactly one release.
func (g *BackpressureGate) releaseFunc() func() {
	return func() {
		g.inFlight.Add(-1)
		<-g.permits
	}
}

// NewBackpressureExecutor wraps inner so that at most maxInFlight writes run
// concurrently. A non-positive maxInFlight returns a passthrough wrapper that
// imposes no bound. observer is optional and, when set, is notified each time a
// write blocks waiting for a permit. The wrapper owns a private gate; use
// NewBackpressureExecutorWithGate to share one pool across paths.
func NewBackpressureExecutor(inner Executor, maxInFlight int, observer BackpressureObserver) *BackpressureExecutor {
	return NewBackpressureExecutorWithGate(inner, NewBackpressureGate(maxInFlight, observer))
}

// NewBackpressureExecutorWithGate wraps inner against an existing shared gate so
// the Executor draws from the same permit pool as other wrappers built on that
// gate. A nil gate is treated as a passthrough.
func NewBackpressureExecutorWithGate(inner Executor, gate *BackpressureGate) *BackpressureExecutor {
	return &BackpressureExecutor{inner: inner, gate: gate}
}

// Gate returns the shared permit pool this executor draws from so a sibling
// wrapper (for example a materializer CypherExecutor) can bound on the same pool.
func (e *BackpressureExecutor) Gate() *BackpressureGate {
	return e.gate
}

// MaxInFlight reports the configured concurrent-write ceiling. A non-positive
// value means backpressure is disabled.
func (e *BackpressureExecutor) MaxInFlight() int {
	return e.gate.MaxInFlight()
}

// InFlight reports the number of writes currently holding a permit. It lets a
// runtime register an observable gauge so an operator can see, at 3 AM, how close
// the write path is to its backpressure ceiling.
func (e *BackpressureExecutor) InFlight() int64 {
	return e.gate.InFlight()
}

// Execute runs one statement under the in-flight bound.
func (e *BackpressureExecutor) Execute(ctx context.Context, statement Statement) error {
	release, err := e.gate.Acquire(ctx, string(statement.Operation))
	if err != nil {
		return err
	}
	defer release()
	return e.inner.Execute(ctx, statement)
}

// ExecuteGroup runs a grouped write under the same in-flight bound as single
// statements, so the grouped path cannot bypass the ceiling. It returns an error
// if Inner does not support grouped writes.
func (e *BackpressureExecutor) ExecuteGroup(ctx context.Context, statements []Statement) error {
	ge, ok := e.inner.(GroupExecutor)
	if !ok {
		return errInnerNoExecuteGroup
	}
	release, err := e.gate.Acquire(ctx, groupOperationLabel(statements))
	if err != nil {
		return err
	}
	defer release()
	return ge.ExecuteGroup(ctx, statements)
}

// executeOnlyBackpressureWrapper wraps BackpressureExecutor but intentionally
// does not implement GroupExecutor. Use it when the inner executor does not
// support grouped writes so callers that type-assert GroupExecutor fall through
// to sequential execution rather than receiving errInnerNoExecuteGroup.
type executeOnlyBackpressureWrapper struct {
	bp *BackpressureExecutor
}

// Execute forwards the statement to the underlying BackpressureExecutor,
// preserving the in-flight bound without exposing ExecuteGroup.
func (w executeOnlyBackpressureWrapper) Execute(ctx context.Context, stmt Statement) error {
	return w.bp.Execute(ctx, stmt)
}

// ExecuteOnlyBackpressureExecutor returns an Executor backed by bp that does
// not expose GroupExecutor. Use when the inner executor is an
// ExecuteOnlyExecutor (ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=false) so
// type assertions for GroupExecutor correctly fall through to sequential
// execution rather than hitting errInnerNoExecuteGroup inside ExecuteGroup.
func ExecuteOnlyBackpressureExecutor(bp *BackpressureExecutor) Executor {
	return executeOnlyBackpressureWrapper{bp: bp}
}
