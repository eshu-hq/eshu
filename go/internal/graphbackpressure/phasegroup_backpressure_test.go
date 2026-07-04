// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphbackpressure

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// phaseGroupCapableExecutor implements cypher.PhaseGroupExecutor (and Execute)
// but deliberately NOT cypher.GroupExecutor, mirroring bootstrap-index's
// canonical executor shape (bootstrapNornicDBPhaseGroupExecutor). Every call is
// tracked so tests can assert peak concurrency and completion.
type phaseGroupCapableExecutor struct {
	mu       sync.Mutex
	current  int
	peak     int
	calls    int
	blockErr error
	release  chan struct{}
}

func (e *phaseGroupCapableExecutor) enter() {
	e.mu.Lock()
	e.current++
	e.calls++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()
}

func (e *phaseGroupCapableExecutor) leave() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *phaseGroupCapableExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

func (e *phaseGroupCapableExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *phaseGroupCapableExecutor) Execute(ctx context.Context, _ cypher.Statement) error {
	return e.run(ctx)
}

func (e *phaseGroupCapableExecutor) ExecutePhaseGroup(ctx context.Context, _ []cypher.Statement) error {
	return e.run(ctx)
}

func (e *phaseGroupCapableExecutor) run(ctx context.Context) error {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.blockErr
}

// TestWrapExecutorWithGatePreservesPhaseGroupExecutor proves the fourth
// WrapExecutorWithGate case: when inner implements cypher.PhaseGroupExecutor
// (bootstrap-index's canonical executor shape) but not cypher.GroupExecutor,
// wrapping it with a gate must still return a value that implements
// cypher.PhaseGroupExecutor, and calling ExecutePhaseGroup on the wrapped value
// must actually route through the inner's ExecutePhaseGroup (not silently
// degrade to Execute). This is the confirmed blocker fix: before Part 1,
// WrapExecutorWithGate only recognized GroupExecutor, so wrapping a
// PhaseGroupExecutor-only inner stripped ExecutePhaseGroup and forced
// CanonicalNodeWriter.Write onto the per-statement sequential Execute fallback.
func TestWrapExecutorWithGatePreservesPhaseGroupExecutor(t *testing.T) {
	t.Parallel()

	inner := &phaseGroupCapableExecutor{}
	gate := NewGate(4, nil, CanonicalGateName)

	wrapped := WrapExecutorWithGate(inner, gate)

	pge, ok := wrapped.(cypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("WrapExecutorWithGate dropped PhaseGroupExecutor for a phase-group-capable inner")
	}
	if _, ok := wrapped.(cypher.GroupExecutor); ok {
		t.Fatal("WrapExecutorWithGate exposed GroupExecutor for an inner that never implemented it")
	}

	if err := pge.ExecutePhaseGroup(context.Background(), []cypher.Statement{{Cypher: "RETURN 1"}}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("inner call count = %d, want 1 (ExecutePhaseGroup must route to inner, not degrade)", got)
	}
}

// TestWrapExecutorWithGatePhaseGroupBoundsConcurrency proves the peak-in-flight
// bound applies to the phase-group path: ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2 with
// many concurrent ExecutePhaseGroup callers must never let more than 2 run the
// inner executor at once.
func TestWrapExecutorWithGatePhaseGroupBoundsConcurrency(t *testing.T) {
	t.Parallel()

	const maxInFlight = 2
	const callers = 10

	inner := &phaseGroupCapableExecutor{release: make(chan struct{})}
	gate := NewGate(maxInFlight, nil, CanonicalGateName)
	wrapped := WrapExecutorWithGate(inner, gate)
	pge, ok := wrapped.(cypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("WrapExecutorWithGate dropped PhaseGroupExecutor")
	}

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pge.ExecutePhaseGroup(context.Background(), []cypher.Statement{{Cypher: "RETURN 1"}})
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

// TestWrapExecutorWithGatePhaseGroupDeadlockFreeUnderPressure is the
// deadlock-freedom regression: N concurrent ExecutePhaseGroup callers, one of
// which blocks then succeeds and one of which fails with a graph-write timeout,
// must all complete (a released permit on both success and error/timeout) and
// the whole run must terminate within a bounded deadline while never exceeding
// MaxInFlight. If a permit ever leaked (e.g. on the timeout branch), the
// remaining callers would starve and the test would hang past the deadline.
func TestWrapExecutorWithGatePhaseGroupDeadlockFreeUnderPressure(t *testing.T) {
	t.Parallel()

	const maxInFlight = 3
	const callers = 20

	inner := &phaseGroupCapableExecutor{release: make(chan struct{})}
	gate := NewGate(maxInFlight, nil, CanonicalGateName)
	wrapped := WrapExecutorWithGate(inner, gate)
	pge, ok := wrapped.(cypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("WrapExecutorWithGate dropped PhaseGroupExecutor")
	}

	timeoutErr := errors.New("graph_write_timeout")
	timeoutInner := &phaseGroupCapableExecutor{blockErr: timeoutErr}
	timeoutWrapped := WrapExecutorWithGate(timeoutInner, gate)
	timeoutPGE, ok := timeoutWrapped.(cypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("WrapExecutorWithGate dropped PhaseGroupExecutor for the timeout-injected inner")
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
					// Every fifth caller hits the injected graph-write timeout on a
					// SEPARATE gate-sharing executor; its permit must still release.
					_ = timeoutPGE.ExecutePhaseGroup(context.Background(), []cypher.Statement{{Cypher: "RETURN 1"}})
					return
				}
				_ = pge.ExecutePhaseGroup(context.Background(), []cypher.Statement{{Cypher: "RETURN 1"}})
			}(i)
		}
		// Let callers pile up against the ceiling, then release the blocking
		// (success-path) inner so every blocked caller can complete.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && inner.peakConcurrency() < 1 {
			time.Sleep(time.Millisecond)
		}
		close(inner.release)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ExecutePhaseGroup calls did not terminate within deadline (permit leak / deadlock)")
	}

	if peak := inner.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent successful ExecutePhaseGroup calls = %d, want <= %d", peak, maxInFlight)
	}
	if peak := timeoutInner.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent timeout-path ExecutePhaseGroup calls = %d, want <= %d", peak, maxInFlight)
	}
}

// TestWrapExecutorWithGatePhaseGroupDisabledIsPassthrough proves the default-off
// contract for bootstrap's canonical gate: an unset/non-positive MaxInFlight
// (nil gate) must return the SAME executor value unchanged, still implementing
// cypher.PhaseGroupExecutor, so bootstrap-index behavior is byte-identical
// until an operator opts in.
func TestWrapExecutorWithGatePhaseGroupDisabledIsPassthrough(t *testing.T) {
	t.Parallel()

	inner := &phaseGroupCapableExecutor{}
	wrapped := WrapExecutorWithGate(inner, nil)

	if wrapped != cypher.Executor(inner) {
		t.Fatalf("WrapExecutorWithGate(_, nil) = %T, want the inner executor unchanged", wrapped)
	}
	if _, ok := wrapped.(cypher.PhaseGroupExecutor); !ok {
		t.Fatal("passthrough wrapper lost PhaseGroupExecutor")
	}
}
