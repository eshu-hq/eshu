// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// phaseGroupOnlyExecutor implements sourcecypher.Executor and
// sourcecypher.PhaseGroupExecutor but deliberately NOT GroupExecutor, mirroring
// bootstrapNornicDBPhaseGroupExecutor (bootstrap's canonical NornicDB executor
// shape). It tracks concurrent in-flight calls so tests can assert the
// backpressure ceiling and blocks on release so tests can pile up callers.
type phaseGroupOnlyExecutor struct {
	mu      sync.Mutex
	current int
	peak    int
	calls   int
	err     error
	release chan struct{}
}

func (e *phaseGroupOnlyExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	return e.run(ctx)
}

func (e *phaseGroupOnlyExecutor) ExecutePhaseGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	return e.run(ctx)
}

func (e *phaseGroupOnlyExecutor) run(ctx context.Context) error {
	e.mu.Lock()
	e.current++
	e.calls++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.current--
		e.mu.Unlock()
	}()

	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.err
}

func (e *phaseGroupOnlyExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

func (e *phaseGroupOnlyExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// TestBoundBootstrapCanonicalExecutorPreservesPhaseGroupExecutor is the
// confirmed-blocker regression: bootstrap's canonical NornicDB executor
// (bootstrapNornicDBPhaseGroupExecutor) implements PhaseGroupExecutor, not
// GroupExecutor. Wrapping it outermost with the graph-write gate must still
// return a value that implements sourcecypher.PhaseGroupExecutor so
// CanonicalNodeWriter.Write keeps using the grouped/phase-group write path
// instead of silently falling back to per-statement sequential Execute.
func TestBoundBootstrapCanonicalExecutorPreservesPhaseGroupExecutor(t *testing.T) {
	t.Parallel()

	inner := &phaseGroupOnlyExecutor{}
	gate := newBootstrapGraphWriteGate(func(key string) string {
		if key == graphbackpressure.MaxInFlightEnv {
			return "4"
		}
		return ""
	}, nil)

	wrapped := gate.boundExecutor(inner)

	pge, ok := wrapped.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("boundExecutor dropped PhaseGroupExecutor for bootstrap's phase-group-only canonical executor")
	}
	if _, ok := wrapped.(sourcecypher.GroupExecutor); ok {
		t.Fatal("boundExecutor exposed GroupExecutor for an inner that never implemented it")
	}
	if err := pge.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("inner call count = %d, want 1 (must route through, not degrade)", got)
	}
}

// TestBoundBootstrapCanonicalExecutorBoundsPeakConcurrency proves
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2 caps concurrent ExecutePhaseGroup calls at 2
// even under many simultaneous callers.
func TestBoundBootstrapCanonicalExecutorBoundsPeakConcurrency(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2
	const callers = 10

	inner := &phaseGroupOnlyExecutor{release: make(chan struct{})}
	gate := newBootstrapGraphWriteGate(func(key string) string {
		if key == graphbackpressure.MaxInFlightEnv {
			return "2"
		}
		return ""
	}, nil)
	wrapped := gate.boundExecutor(inner)
	pge, ok := wrapped.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("boundExecutor dropped PhaseGroupExecutor")
	}

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pge.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && inner.peakConcurrency() < maxInFlight {
		time.Sleep(time.Millisecond)
	}
	close(inner.release)
	wg.Wait()

	if peak := inner.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent ExecutePhaseGroup calls = %d, want <= %d", peak, maxInFlight)
	}
	if got := inner.callCount(); got != callers {
		t.Fatalf("inner call count = %d, want %d (every caller must complete)", got, callers)
	}
}

// TestBoundBootstrapCanonicalExecutorTerminatesUnderMixedPressure is the
// deadlock-freedom regression: concurrent ExecutePhaseGroup callers, some
// succeeding after blocking and some failing immediately with an injected
// graph-write-timeout-shaped error, must all complete (permit released on
// success AND error) within a bounded deadline, never exceeding MaxInFlight.
func TestBoundBootstrapCanonicalExecutorTerminatesUnderMixedPressure(t *testing.T) {
	t.Parallel()

	const maxInFlight = 3
	const callers = 20

	gate := newBootstrapGraphWriteGate(func(key string) string {
		if key == graphbackpressure.MaxInFlightEnv {
			return "3"
		}
		return ""
	}, nil)

	blocking := &phaseGroupOnlyExecutor{release: make(chan struct{})}
	blockingWrapped := gate.boundExecutor(blocking)
	blockingPGE, ok := blockingWrapped.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("boundExecutor dropped PhaseGroupExecutor for the blocking inner")
	}

	timeoutErr := timeoutShapedError{}
	failing := &phaseGroupOnlyExecutor{err: timeoutErr}
	failingWrapped := gate.boundExecutor(failing)
	failingPGE, ok := failingWrapped.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("boundExecutor dropped PhaseGroupExecutor for the failing inner")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				if n%5 == 0 {
					_ = failingPGE.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}})
					return
				}
				_ = blockingPGE.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}})
			}(i)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && blocking.peakConcurrency() < 1 {
			time.Sleep(time.Millisecond)
		}
		close(blocking.release)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ExecutePhaseGroup calls did not terminate within deadline (permit leak / deadlock)")
	}

	if peak := blocking.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent successful calls = %d, want <= %d", peak, maxInFlight)
	}
	if peak := failing.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent timeout-path calls = %d, want <= %d", peak, maxInFlight)
	}
}

// TestBoundBootstrapCanonicalExecutorDisabledIsPassthrough proves the
// default-off contract: an unset ESHU_GRAPH_WRITE_MAX_IN_FLIGHT /
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT leaves the returned executor
// unchanged (nil gate, passthrough), so bootstrap-index behavior is
// byte-identical until an operator opts in.
func TestBoundBootstrapCanonicalExecutorDisabledIsPassthrough(t *testing.T) {
	t.Parallel()

	inner := &phaseGroupOnlyExecutor{}
	gate := newBootstrapGraphWriteGate(func(string) string { return "" }, nil)

	wrapped := gate.boundExecutor(inner)
	if wrapped != sourcecypher.Executor(inner) {
		t.Fatalf("boundExecutor with disabled gate = %T, want inner unchanged", wrapped)
	}
}

// timeoutShapedError is a minimal stand-in for a graph-write timeout error
// used only to prove the permit releases on the error path; it does not need
// to implement Retryable() for this backpressure-only regression.
type timeoutShapedError struct{}

func (timeoutShapedError) Error() string { return "graph_write_timeout" }
