package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/app"
)

// blockingRunner is a test app.Runner used to exercise compositeRunner drain
// and error-aggregation behavior. It records whether it observed a
// context-driven shutdown so tests can assert that siblings drained cleanly
// instead of being killed mid-unit.
type blockingRunner struct {
	// returnErr, when non-nil, is returned immediately without waiting for
	// cancellation. It models a sibling that fails fatally.
	returnErr error
	// drainDelay delays the clean return after cancellation to model a sibling
	// that needs bounded time to finish an in-flight unit before exiting.
	drainDelay time.Duration
	// blockForever ignores context cancellation entirely. It models a wedged
	// sibling and is used to prove Run does not wait past the drain grace.
	blockForever bool

	started   chan struct{}
	startOnce sync.Once
	sawCancel atomic.Bool
	returned  atomic.Bool
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{started: make(chan struct{})}
}

func (r *blockingRunner) Run(ctx context.Context) error {
	r.startOnce.Do(func() { close(r.started) })
	defer r.returned.Store(true)

	if r.returnErr != nil {
		return r.returnErr
	}
	if r.blockForever {
		select {}
	}

	<-ctx.Done()
	r.sawCancel.Store(true)
	if r.drainDelay > 0 {
		time.Sleep(r.drainDelay)
	}
	return nil
}

func (r *blockingRunner) awaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-r.started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start within timeout")
	}
}

// TestCompositeRunnerDoesNotMaskFatalSiblingError proves the nil-masking
// regression is fixed. A healthy sibling returns nil on cancellation while
// another returns a fatal error. The old implementation acted on whichever
// result arrived first and could return the healthy nil, dropping the fatal
// error. Run must always surface the fatal error.
func TestCompositeRunnerDoesNotMaskFatalSiblingError(t *testing.T) {
	t.Parallel()

	fatalErr := errors.New("sibling fatal failure")

	healthy := newBlockingRunner()
	fatal := newBlockingRunner()
	fatal.returnErr = fatalErr

	runner := newCompositeRunnerWithGrace(2*time.Second, nil, healthy, fatal)

	err := runner.Run(context.Background())
	if !errors.Is(err, fatalErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, fatalErr)
	}
	if !healthy.sawCancel.Load() {
		t.Fatal("healthy sibling was not canceled on fatal error")
	}
	if !healthy.returned.Load() {
		t.Fatal("healthy sibling did not return after drain")
	}
}

// TestCompositeRunnerJoinsAllTerminalErrors proves Run aggregates every
// sibling's terminal error with errors.Join rather than returning only the
// first error received.
func TestCompositeRunnerJoinsAllTerminalErrors(t *testing.T) {
	t.Parallel()

	errA := errors.New("collector terminal failure")
	errB := errors.New("projector terminal failure")

	a := newBlockingRunner()
	a.returnErr = errA
	b := newBlockingRunner()
	b.returnErr = errB

	runner := newCompositeRunnerWithGrace(2*time.Second, nil, a, b)

	err := runner.Run(context.Background())
	if !errors.Is(err, errA) {
		t.Fatalf("Run() error = %v, want to contain %v", err, errA)
	}
	if !errors.Is(err, errB) {
		t.Fatalf("Run() error = %v, want to contain %v", err, errB)
	}
}

// TestCompositeRunnerGracefullyDrainsSiblingOnFatal proves that on a fatal
// error the surviving sibling is given a bounded grace window to drain cleanly
// (return nil) before Run returns, and that the fatal error is still surfaced.
func TestCompositeRunnerGracefullyDrainsSiblingOnFatal(t *testing.T) {
	t.Parallel()

	fatalErr := errors.New("fatal during steady state")

	healthy := newBlockingRunner()
	healthy.drainDelay = 50 * time.Millisecond // drains well within grace
	fatal := newBlockingRunner()
	fatal.returnErr = fatalErr

	runner := newCompositeRunnerWithGrace(2*time.Second, nil, healthy, fatal)

	err := runner.Run(context.Background())
	if !errors.Is(err, fatalErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, fatalErr)
	}
	if !healthy.sawCancel.Load() {
		t.Fatal("healthy sibling should have been asked to drain via cancellation")
	}
	if !healthy.returned.Load() {
		t.Fatal("healthy sibling should have drained and returned within grace window")
	}
}

// TestCompositeRunnerBoundsDrainOnWedgedSibling proves Run returns within the
// bounded grace window even when a sibling ignores cancellation, and that it
// surfaces both the original fatal error and a drain-timeout marker rather than
// blocking forever.
func TestCompositeRunnerBoundsDrainOnWedgedSibling(t *testing.T) {
	t.Parallel()

	fatalErr := errors.New("fatal with wedged sibling")

	wedged := newBlockingRunner()
	wedged.blockForever = true
	fatal := newBlockingRunner()
	fatal.returnErr = fatalErr

	runner := newCompositeRunnerWithGrace(100*time.Millisecond, nil, wedged, fatal)

	done := make(chan error, 1)
	go func() { done <- runner.Run(context.Background()) }()

	select {
	case err := <-done:
		if !errors.Is(err, fatalErr) {
			t.Fatalf("Run() error = %v, want to contain %v", err, fatalErr)
		}
		if !errors.Is(err, errCompositeDrainTimeout) {
			t.Fatalf("Run() error = %v, want to contain drain-timeout marker", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within bounded drain window for wedged sibling")
	}
}

// TestCompositeRunnerCleanShutdownReturnsNil proves that when all runners
// return nil after an external context cancellation, Run returns nil without
// inventing an error or treating a clean exit as fatal.
func TestCompositeRunnerCleanShutdownReturnsNil(t *testing.T) {
	t.Parallel()

	a := newBlockingRunner()
	b := newBlockingRunner()

	runner := newCompositeRunnerWithGrace(2*time.Second, nil, a, b)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	a.awaitStarted(t)
	b.awaitStarted(t)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil on clean shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return promptly after external cancel")
	}
}

// TestCompositeRunnerNoGoroutineLeak proves Run leaves no lingering goroutines
// after a clean shutdown. Run with -race to also catch data races on the shared
// error aggregation.
func TestCompositeRunnerNoGoroutineLeak(t *testing.T) {
	t.Parallel()

	before := runtime.NumGoroutine()

	a := newBlockingRunner()
	b := newBlockingRunner()

	runner := newCompositeRunnerWithGrace(2*time.Second, nil, a, b)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	a.awaitStarted(t)
	b.awaitStarted(t)
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	waitForGoroutines(t, before)
}

func waitForGoroutines(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: started near %d, still %d after wait", baseline, runtime.NumGoroutine())
}

var _ app.Runner = (*blockingRunner)(nil)
