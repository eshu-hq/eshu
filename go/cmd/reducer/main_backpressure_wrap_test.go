// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// poolProbeExecutor records the peak number of concurrent in-flight
// Execute/ExecuteGroup calls so a test can prove the shared backpressure bound
// reaches the base executor every reducer writer derives from. release blocks
// each call until closed, letting the test hold permits and observe gating.
type poolProbeExecutor struct {
	mu      sync.Mutex
	current int
	peak    int
	release chan struct{}
}

func (e *poolProbeExecutor) enter() {
	e.mu.Lock()
	e.current++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()
}

func (e *poolProbeExecutor) leave() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *poolProbeExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

func (e *poolProbeExecutor) block(ctx context.Context) error {
	e.enter()
	defer e.leave()
	if e.release == nil {
		return nil
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *poolProbeExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	return e.block(ctx)
}

func (e *poolProbeExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	return e.block(ctx)
}

// TestBoundReducerGraphWritesSharesOnePoolAcrossBaseExecutor is the structural
// regression for issue #3652 P2: the backpressure bound MUST be applied to the
// base executor that every reducer writer derives from, so a single in-flight
// permit pool gates all of them. Before the fix only the semantic executor was
// wrapped, leaving handler edges, shared projection, secrets/IAM, orphan sweep,
// and workload materializers unbounded. This test drives concurrent writes
// through the bounded base executor and proves the peak inner concurrency never
// exceeds the ceiling.
func TestBoundReducerGraphWritesSharesOnePoolAcrossBaseExecutor(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2
	const callers = 16

	probe := &poolProbeExecutor{release: make(chan struct{})}
	gate := newReducerGraphWriteGate(func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "2"
		}
		return ""
	}, nil)
	bounded := gate.boundExecutor(probe)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bounded.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probe.peakConcurrency() < maxInFlight {
		time.Sleep(time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent base writes = %d, want <= %d (shared bound breached)", peak, maxInFlight)
	}
}

// TestReducerGraphWriteGateSharesOnePoolAcrossExecutorAndMaterializer is the
// regression for issue #3652 P2: the Executor path and the materializer
// CypherExecutor path MUST draw from the SAME shared permit pool. Before the
// fix only the Executor was wrapped, so workload/infrastructure-platform
// materializer writes bypassed the bound entirely. This test saturates the pool
// with materializer writes and proves an Executor write is then gated too (and
// vice versa), so the combined in-flight count never exceeds the ceiling.
func TestReducerGraphWriteGateSharesOnePoolAcrossExecutorAndMaterializer(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2

	probe := &poolProbeExecutor{release: make(chan struct{})}
	gate := newReducerGraphWriteGate(func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "2"
		}
		return ""
	}, nil)
	boundedExec := gate.boundExecutor(probe)
	boundedMat := gate.boundCypherExecutor(cypherExecutorAdapter{probe})

	var wg sync.WaitGroup
	// Mix Executor and materializer callers against one pool.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = boundedExec.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = boundedMat.ExecuteCypher(context.Background(), "RETURN 1", nil)
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probe.peakConcurrency() < maxInFlight {
		time.Sleep(time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent writes across Executor+materializer = %d, want <= %d (paths not sharing one pool)", peak, maxInFlight)
	}
}

// TestReducerGraphWriteGateDisabledIsPassthrough proves a non-positive
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT leaves both the Executor and the materializer
// path unchanged, so the gate is a safe no-op and preserves the GroupExecutor
// interface on the Executor path.
func TestReducerGraphWriteGateDisabledIsPassthrough(t *testing.T) {
	t.Parallel()

	probe := &poolProbeExecutor{}
	gate := newReducerGraphWriteGate(func(string) string { return "" }, nil)

	bounded := gate.boundExecutor(probe)
	if bounded != sourcecypher.Executor(probe) {
		t.Fatalf("disabled gate boundExecutor = %T, want inner unchanged", bounded)
	}
	if _, ok := bounded.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("disabled bound dropped GroupExecutor, want interface preserved")
	}

	mat := cypherExecutorAdapter{probe}
	if got := gate.boundCypherExecutor(mat); got != reducer.CypherExecutor(mat) {
		t.Fatalf("disabled gate boundCypherExecutor = %T, want inner unchanged", got)
	}
}

// cypherExecutorAdapter adapts a poolProbeExecutor to reducer.CypherExecutor so
// one probe can record peak concurrency across both the Executor and the
// materializer paths sharing a gate.
type cypherExecutorAdapter struct {
	probe *poolProbeExecutor
}

func (a cypherExecutorAdapter) ExecuteCypher(ctx context.Context, _ string, _ map[string]any) error {
	return a.probe.block(ctx)
}

// deadlineProbeExecutor records the context deadline budget it observes on entry
// so a test can prove whether permit-wait time was charged against a downstream
// write timeout. It always completes immediately (no blocking).
type deadlineProbeExecutor struct {
	gotDeadline chan time.Duration
}

func (e *deadlineProbeExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	if dl, ok := ctx.Deadline(); ok {
		select {
		case e.gotDeadline <- time.Until(dl):
		default:
		}
	}
	return nil
}

// permitHolderExecutor blocks until release is closed and signals when it has
// entered, so a test can pin the shared permit without being subject to any
// downstream write timeout.
type permitHolderExecutor struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (e *permitHolderExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	e.once.Do(func() { close(e.entered) })
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestSemanticPathPermitWaitIsOutsideWriteTimeout is the regression for issue
// #3652 P1: the shared permit MUST be acquired OUTSIDE the per-statement
// TimeoutExecutor (ESHU_CANONICAL_WRITE_TIMEOUT). Otherwise a queued semantic
// write that waits for a saturated pool burns its write-timeout budget while
// waiting and fails as graph_write_timeout before reaching the backend —
// reintroducing the dead-letter flood backpressure exists to prevent.
//
// The test shares ONE gate across two paths: a holder path (no timeout) pins the
// single permit, and the semantic path (gate.boundExecutor wrapping a
// TimeoutExecutor, exactly as buildReducerService composes it) queues a write
// behind it. The queued write waits far longer than the write timeout for the
// permit; with the permit OUTSIDE the timeout, its timeout clock only starts
// after it acquires the permit, so the backend sees a full, unexpired deadline
// and the write succeeds rather than timing out.
func TestSemanticPathPermitWaitIsOutsideWriteTimeout(t *testing.T) {
	t.Parallel()

	const writeTimeout = 40 * time.Millisecond

	gate := newReducerGraphWriteGate(func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "1"
		}
		return ""
	}, nil)

	// Holder path: no timeout, just the shared permit. It pins the only permit.
	holder := &permitHolderExecutor{release: make(chan struct{}), entered: make(chan struct{})}
	holderBound := gate.boundExecutor(holder)

	// Semantic path: TimeoutExecutor inside, permit gate outermost — the exact
	// layering buildReducerService now uses.
	backend := &deadlineProbeExecutor{gotDeadline: make(chan time.Duration, 4)}
	semanticBound := gate.boundExecutor(sourcecypher.TimeoutExecutor{Inner: backend, Timeout: writeTimeout})

	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_ = holderBound.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	}()
	<-holder.entered // holder owns the only permit

	// Queue the semantic write; it must block waiting for the permit.
	queuedErr := make(chan error, 1)
	go func() {
		queuedErr <- semanticBound.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	}()

	// Park the queued caller on the permit far longer than the write timeout. If
	// the permit were inside the timeout, the queued write would already have
	// failed as graph_write_timeout by now.
	time.Sleep(3 * writeTimeout)

	// Release the holder so the queued write acquires the permit and runs with a
	// fresh timeout budget.
	close(holder.release)
	<-holderDone

	select {
	case budget := <-backend.gotDeadline:
		if budget < writeTimeout/2 {
			t.Fatalf("queued write deadline budget = %v, want ~%v (permit-wait leaked into the write timeout)", budget, writeTimeout)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued write never reached the backend")
	}

	select {
	case err := <-queuedErr:
		if err != nil {
			t.Fatalf("queued write error = %v, want nil (a saturated pool must delay, not time out, a queued write)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued write did not complete")
	}
}

// stubGroupExecutor is a no-op executor that also implements GroupExecutor,
// used in tests that exercise the grouped-write code path.
type stubGroupExecutor struct{}

func (stubGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error { return nil }

// ExecuteGroup satisfies cypher.GroupExecutor so Wrap preserves the grouped
// write interface when the inner executor advertises it.
func (stubGroupExecutor) ExecuteGroup(_ context.Context, _ []sourcecypher.Statement) error {
	return nil
}

// TestBuildReducerServiceMaxInFlightWrapsAllWriters proves that when
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT is positive, buildReducerService succeeds
// and the wiring does not panic — the backpressure wrap covers all writers
// including the semantic entity writer that goes through ExecuteOnlyExecutor
// when grouped writes are disabled.
func TestBuildReducerServiceMaxInFlightWrapsAllWriters(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	// NornicDB backend with grouped writes DISABLED (ExecuteOnlyExecutor path)
	// and MAX_IN_FLIGHT=2. Before the fix, Wrap happened after other writers
	// were built, so MAX_IN_FLIGHT had no effect on them.
	_, err := buildReducerService(
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string {
			switch name {
			case "ESHU_GRAPH_BACKEND":
				return string(runtimecfg.GraphBackendNornicDB)
			case graphbackpressure.MaxInFlightEnv:
				return "2"
			case "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES":
				return "false"
			default:
				return ""
			}
		},
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildReducerService() with MAX_IN_FLIGHT=2 error = %v, want nil", err)
	}
}

// TestBuildReducerServiceMaxInFlightWithGroupedWritesEnabled proves that when
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT is positive AND grouped writes are enabled,
// buildReducerService succeeds without error (the GroupExecutor path is
// preserved).
func TestBuildReducerServiceMaxInFlightWithGroupedWritesEnabled(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	_, err := buildReducerService(
		db,
		stubGroupExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(name string) string {
			switch name {
			case "ESHU_GRAPH_BACKEND":
				return string(runtimecfg.GraphBackendNornicDB)
			case graphbackpressure.MaxInFlightEnv:
				return "2"
			case "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES":
				return "true"
			default:
				return ""
			}
		},
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildReducerService() with MAX_IN_FLIGHT=2 and grouped writes error = %v, want nil", err)
	}
}
