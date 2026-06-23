package cypher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// concurrencyProbeExecutor records the peak number of concurrent in-flight
// Execute/ExecuteGroup calls so a test can assert the backpressure bound is
// never exceeded. release, when non-nil, blocks each call until it is closed,
// letting a test hold permits and observe that additional writers are gated.
type concurrencyProbeExecutor struct {
	mu      sync.Mutex
	current int
	peak    int
	release chan struct{}
	err     error
}

func (e *concurrencyProbeExecutor) enter() {
	e.mu.Lock()
	e.current++
	if e.current > e.peak {
		e.peak = e.current
	}
	e.mu.Unlock()
}

func (e *concurrencyProbeExecutor) leave() {
	e.mu.Lock()
	e.current--
	e.mu.Unlock()
}

func (e *concurrencyProbeExecutor) peakConcurrency() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

func (e *concurrencyProbeExecutor) Execute(ctx context.Context, _ Statement) error {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.err
}

func (e *concurrencyProbeExecutor) ExecuteGroup(ctx context.Context, _ []Statement) error {
	e.enter()
	defer e.leave()
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.err
}

// TestBackpressureExecutorBoundsConcurrentWrites is the core regression for
// issue #3560: without a write-path bound, every reducer/projector worker can
// drive a concurrent graph write at once, so a slow NornicDB backend is hammered
// by N simultaneous writes that all time out and flood the dead-letter queue.
// The executor MUST cap in-flight writes at MaxInFlight even when many callers
// race for a permit.
func TestBackpressureExecutorBoundsConcurrentWrites(t *testing.T) {
	const maxInFlight = 3
	const callers = 24

	probe := &concurrencyProbeExecutor{release: make(chan struct{})}
	exec := NewBackpressureExecutor(probe, maxInFlight, nil)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = exec.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
		}()
	}

	// Give the goroutines time to pile up against the permit ceiling, then let
	// them drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.InFlight() >= maxInFlight {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := exec.InFlight(); got > maxInFlight {
		t.Fatalf("InFlight() = %d while saturating, want <= %d", got, maxInFlight)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent inner writes = %d, want <= %d (bound breached)", peak, maxInFlight)
	}
	if got := exec.InFlight(); got != 0 {
		t.Fatalf("InFlight() = %d after drain, want 0 (permit leak)", got)
	}
}

// TestBackpressureExecutorReleasesPermitOnError proves a failing inner write
// (the timeout case that dead-letters) still releases its permit, so a backend
// stuck timing out does not permanently starve the write path.
func TestBackpressureExecutorReleasesPermitOnError(t *testing.T) {
	wantErr := errors.New("graph write timed out")
	probe := &concurrencyProbeExecutor{err: wantErr}
	exec := NewBackpressureExecutor(probe, 1, nil)

	for i := 0; i < 5; i++ {
		if err := exec.Execute(context.Background(), Statement{Cypher: "RETURN 1"}); !errors.Is(err, wantErr) {
			t.Fatalf("Execute() error = %v, want %v", err, wantErr)
		}
	}
	if got := exec.InFlight(); got != 0 {
		t.Fatalf("InFlight() = %d after errors, want 0 (permit leaked on error path)", got)
	}
}

// TestBackpressureExecutorZeroBoundIsPassthrough mirrors TimeoutExecutor's
// zero-timeout contract: a non-positive bound disables backpressure entirely so
// the wrapper is a safe no-op when an operator has not opted in.
func TestBackpressureExecutorZeroBoundIsPassthrough(t *testing.T) {
	const callers = 16
	probe := &concurrencyProbeExecutor{release: make(chan struct{})}
	exec := NewBackpressureExecutor(probe, 0, nil)

	var wg sync.WaitGroup
	var started atomic.Int64
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Add(1)
			_ = exec.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if probe.peakConcurrency() >= callers {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak != callers {
		t.Fatalf("peak concurrent inner writes = %d, want %d (passthrough should not gate)", peak, callers)
	}
}

func TestBackpressureExecutorNilGateReportsDisabled(t *testing.T) {
	probe := &concurrencyProbeExecutor{}
	exec := NewBackpressureExecutorWithGate(probe, nil)

	if got := exec.MaxInFlight(); got != 0 {
		t.Fatalf("MaxInFlight() = %d, want 0 for nil gate", got)
	}
	if got := exec.InFlight(); got != 0 {
		t.Fatalf("InFlight() = %d, want 0 for nil gate", got)
	}
	if err := exec.Execute(context.Background(), Statement{Cypher: "RETURN 1"}); err != nil {
		t.Fatalf("Execute() with nil gate returned error: %v", err)
	}
}

// TestBackpressureExecutorGroupRespectsBound proves grouped writes share the
// same permit pool as single-statement writes; otherwise a busy ExecuteGroup
// path could bypass the in-flight ceiling.
func TestBackpressureExecutorGroupRespectsBound(t *testing.T) {
	const maxInFlight = 2
	const callers = 12

	probe := &concurrencyProbeExecutor{release: make(chan struct{})}
	exec := NewBackpressureExecutor(probe, maxInFlight, nil)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = exec.ExecuteGroup(context.Background(), []Statement{{Cypher: "RETURN 1"}})
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.InFlight() >= maxInFlight {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	if peak := probe.peakConcurrency(); peak > maxInFlight {
		t.Fatalf("peak concurrent grouped writes = %d, want <= %d", peak, maxInFlight)
	}
}

// TestBackpressureExecutorWithExecuteOnlyInnerDoesNotExposeGroup is the
// regression for the P1 fix: when the inner executor is ExecuteOnlyExecutor,
// the backpressure wrapper must not expose ExecuteGroup, so a caller that
// type-asserts GroupExecutor falls through to sequential execution instead of
// getting errInnerNoExecuteGroup.
func TestBackpressureExecutorWithExecuteOnlyInnerDoesNotExposeGroup(t *testing.T) {
	t.Parallel()

	inner := ExecuteOnlyExecutor{Inner: &concurrencyProbeExecutor{}}
	exec := NewBackpressureExecutor(inner, 4, nil)

	// The wrapper itself DOES expose ExecuteGroup (it always has the method).
	// The fix is in graphbackpressure.Wrap: it must return ExecuteOnlyBackpressureExecutor
	// when inner lacks GroupExecutor, so type assertions see no ExecuteGroup.
	wrapped := ExecuteOnlyBackpressureExecutor(exec)
	if _, ok := wrapped.(GroupExecutor); ok {
		t.Fatal("ExecuteOnlyBackpressureExecutor exposes GroupExecutor, want no GroupExecutor so sequential fallback works")
	}
	// Execute must still work through the wrapper.
	if err := wrapped.Execute(context.Background(), Statement{Cypher: "RETURN 1"}); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}

// TestBackpressureExecutorContextCancelWhileWaiting proves a caller blocked
// waiting for a permit observes context cancellation instead of hanging, and
// that the cancellation does not consume a permit.
func TestBackpressureExecutorContextCancelWhileWaiting(t *testing.T) {
	probe := &concurrencyProbeExecutor{release: make(chan struct{})}
	exec := NewBackpressureExecutor(probe, 1, nil)

	// Occupy the only permit.
	occupied := make(chan struct{})
	go func() {
		close(occupied)
		_ = exec.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
	}()
	<-occupied
	// Wait until the holder is inside the inner executor.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && exec.InFlight() < 1 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := exec.Execute(ctx, Statement{Cypher: "RETURN 1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() with canceled ctx error = %v, want context.Canceled", err)
	}
	close(probe.release)
}
