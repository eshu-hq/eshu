package collector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSchedulerRolePrefersGiantOverFrontLoadedSmallLane proves the #3839
// guarantee deterministically at the scheduler level, free of the startStream
// classifier-vs-worker startup race.
//
// It reproduces codex's worst case directly: BOTH lanes are pre-filled (as if
// the classifier had fully drained the small lane into smallCh before any worker
// ran) and then closed, and a single worker of each role drains them. A
// large-preferring worker must process the giant FIRST — runLargePreferring
// reserves a semaphore slot and block-prefers the large lane, so it never grabs
// a small while a giant is available. A small-preferring worker processes a
// small first and only reaches the giant after the small lane drains.
//
// The contrast is the test's teeth: a regression that made the dedicated worker
// behave like the small-preferring loop would start the giant last and fail the
// first assertion; the second assertion proves the scenario actually
// discriminates (a small-preferring worker really does start a small first).
func TestSchedulerRolePrefersGiantOverFrontLoadedSmallLane(t *testing.T) {
	t.Parallel()

	const giant = "repo-giant"
	smalls := []string{"s0", "s1", "s2", "s3", "s4", "s5"}

	firstProcessed := func(t *testing.T, role func(*snapshotScheduler, int)) string {
		t.Helper()

		snapshots := map[string]RepositorySnapshot{giant: {RepoPath: giant, FileCount: 50}}
		for _, s := range smalls {
			snapshots[s] = RepositorySnapshot{RepoPath: s, FileCount: 0}
		}
		stub := &stubRepositorySnapshotter{snapshots: snapshots}

		stream := make(chan CollectedGeneration, len(smalls)+1)
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for gen := range stream {
				drainFactChannel(gen.Facts)
			}
		}()

		src := &GitSource{Component: "collector-git", Snapshotter: stub}
		src.stream = stream

		// Pre-fill both lanes fully (the worst-case "classifier already drained"
		// ordering), then close so the single worker drains both and exits.
		smallCh := make(chan SelectedRepository, len(smalls))
		for _, s := range smalls {
			smallCh <- SelectedRepository{RepoPath: s, RemoteURL: "https://github.com/example/repo"}
		}
		close(smallCh)
		largeCh := make(chan SelectedRepository, 1)
		largeCh <- SelectedRepository{RepoPath: giant, RemoteURL: "https://github.com/example/repo"}
		close(largeCh)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var (
			once      sync.Once
			firstErr  error
			completed atomic.Int64
		)
		sc := &snapshotScheduler{
			source:      src,
			smallCh:     smallCh,
			largeCh:     largeCh,
			largeSem:    make(chan struct{}, 1),
			workerCtx:   ctx,
			cancel:      cancel,
			sourceRunID: "run-1",
			observedAt:  time.Unix(0, 0).UTC(),
			errOnce:     &once,
			firstErr:    &firstErr,
			completed:   &completed,
		}

		role(sc, 1)
		close(stream)
		<-drained

		if firstErr != nil {
			t.Fatalf("worker error = %v", firstErr)
		}
		stub.mu.Lock()
		defer stub.mu.Unlock()
		if len(stub.calls) != len(smalls)+1 {
			t.Fatalf("processed %d repos, want %d (all repos must drain)", len(stub.calls), len(smalls)+1)
		}
		return stub.calls[0]
	}

	if first := firstProcessed(t, (*snapshotScheduler).runLargePreferring); first != giant {
		t.Fatalf("large-preferring worker processed %q first, want giant %q (early giant start not guaranteed)",
			first, giant)
	}
	if first := firstProcessed(t, (*snapshotScheduler).runSmallPreferring); first == giant {
		t.Fatalf("small-preferring worker processed the giant first; expected a small (scenario does not discriminate)")
	}
}

// concurrencyPeakSnapshotter is a RepositorySnapshotter that records the peak
// number of SnapshotRepository calls executing concurrently. A brief sleep
// inside SnapshotRepository ensures real overlap when the semaphore allows
// multiple concurrent holders.
type concurrencyPeakSnapshotter struct {
	mu        sync.Mutex
	inflight  int
	peak      int
	calls     []string
	snapshots map[string]RepositorySnapshot
}

// SnapshotRepository satisfies RepositorySnapshotter.
func (c *concurrencyPeakSnapshotter) SnapshotRepository(
	_ context.Context, repo SelectedRepository,
) (RepositorySnapshot, error) {
	c.mu.Lock()
	c.inflight++
	if c.inflight > c.peak {
		c.peak = c.inflight
	}
	c.calls = append(c.calls, repo.RepoPath)
	c.mu.Unlock()

	// Hold the slot long enough for other goroutines to be scheduled.
	time.Sleep(5 * time.Millisecond)

	c.mu.Lock()
	c.inflight--
	c.mu.Unlock()

	snap := c.snapshots[repo.RepoPath]
	return snap, nil
}

// TestSchedulerSemaphoreCapNotExceeded proves that the largeSem capacity is
// never breached when multiple large-preferring workers run concurrently.
//
// It fails a regression where the semaphore acquisition logic is bypassed
// (e.g. the slot send is guarded by a condition that allows more than cap
// concurrent holders), because the peak concurrent count recorded by the
// instrumented snapshotter would exceed the semaphore cap.
func TestSchedulerSemaphoreCapNotExceeded(t *testing.T) {
	t.Parallel()

	const semCap = 2
	giants := []string{"giant-0", "giant-1", "giant-2", "giant-3", "giant-4"}

	snapshots := make(map[string]RepositorySnapshot, len(giants))
	for _, g := range giants {
		snapshots[g] = RepositorySnapshot{RepoPath: g, FileCount: 9999}
	}

	cts := &concurrencyPeakSnapshotter{snapshots: snapshots}

	// Build the scheduler directly — same pattern as the existing test.
	largeCh := make(chan SelectedRepository, len(giants))
	for _, g := range giants {
		largeCh <- SelectedRepository{RepoPath: g, RemoteURL: "https://github.com/example/repo"}
	}
	close(largeCh)
	smallCh := make(chan SelectedRepository)
	close(smallCh)

	stream := make(chan CollectedGeneration, len(giants)+1)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for gen := range stream {
			drainFactChannel(gen.Facts)
		}
	}()

	src := &GitSource{Component: "collector-git", Snapshotter: cts}
	src.stream = stream

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		once      sync.Once
		firstErr  error
		completed atomic.Int64
	)
	sc := &snapshotScheduler{
		source:      src,
		smallCh:     smallCh,
		largeCh:     largeCh,
		largeSem:    make(chan struct{}, semCap),
		workerCtx:   ctx,
		cancel:      cancel,
		sourceRunID: "run-cap",
		observedAt:  time.Unix(0, 0).UTC(),
		errOnce:     &once,
		firstErr:    &firstErr,
		completed:   &completed,
	}

	// Run 3 large-preferring workers concurrently (> semCap) so contention is real.
	var wg sync.WaitGroup
	const numWorkers = 3
	for i := range numWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sc.runLargePreferring(id + 1)
		}(i)
	}
	wg.Wait()
	close(stream)
	<-drainDone

	if firstErr != nil {
		t.Fatalf("unexpected worker error: %v", firstErr)
	}

	// All giants must be processed exactly once.
	cts.mu.Lock()
	gotCalls := len(cts.calls)
	peakConcurrent := cts.peak
	cts.mu.Unlock()

	if gotCalls != len(giants) {
		t.Fatalf("processed %d giants, want %d", gotCalls, len(giants))
	}
	if peakConcurrent > semCap {
		t.Fatalf("peak concurrent giant snapshots = %d, exceeded semaphore cap %d (semaphore not enforced)",
			peakConcurrent, semCap)
	}
}

// TestSchedulerNoSemaphoreLeakOnCtxCancel proves that cancelling the worker
// context mid-flight does not leave the large-repo semaphore permanently
// occupied (which would hang any subsequent acquire).
//
// It fails a regression where runHeldGiantSlot releases the slot on the
// ctx-cancel branch is removed, because the post-cancel acquire attempt
// would block forever (select on a full channel).
func TestSchedulerNoSemaphoreLeakOnCtxCancel(t *testing.T) {
	t.Parallel()

	// largeCh is intentionally NOT closed — the worker blocks in
	// runHeldGiantSlot waiting for a repo or ctx cancellation.
	largeCh := make(chan SelectedRepository) // unbuffered, never written
	smallCh := make(chan SelectedRepository)
	close(smallCh)

	stream := make(chan CollectedGeneration, 1)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for gen := range stream {
			drainFactChannel(gen.Facts)
		}
	}()

	src := &GitSource{
		Component:   "collector-git",
		Snapshotter: &stubRepositorySnapshotter{snapshots: map[string]RepositorySnapshot{}},
	}
	src.stream = stream

	ctx, cancel := context.WithCancel(context.Background())
	var (
		once      sync.Once
		firstErr  error
		completed atomic.Int64
	)
	sem := make(chan struct{}, 1)
	sc := &snapshotScheduler{
		source:      src,
		smallCh:     smallCh,
		largeCh:     largeCh,
		largeSem:    sem,
		workerCtx:   ctx,
		cancel:      cancel,
		sourceRunID: "run-leak",
		observedAt:  time.Unix(0, 0).UTC(),
		errOnce:     &once,
		firstErr:    &firstErr,
		completed:   &completed,
	}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		sc.runLargePreferring(1)
	}()

	// Give the worker time to acquire the slot and block on largeCh.
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Worker must return within a generous deadline — a semaphore leak would
	// cause it to hang forever on the next loop iteration's slot acquire.
	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runLargePreferring did not return after ctx cancel (possible semaphore leak / hang)")
	}

	close(stream)
	<-drainDone

	// Semaphore must be fully released: len==0 means the slot was returned.
	if got := len(sem); got != 0 {
		t.Fatalf("largeSem has %d token(s) after ctx cancel, want 0 (slot not released)", got)
	}
}

// TestSchedulerSemaphoreReleasedOnSnapshotError proves that a snapshot error
// does not leave the large-repo semaphore occupied. If the afterSnapshot
// callback were not invoked on the error path inside processRepo, the slot
// would never be returned and any subsequent acquire would deadlock.
//
// It fails a regression where afterSnapshot is guarded by `if err == nil`
// (or similar), because the post-error len(sem) check would see 1 instead of 0.
func TestSchedulerSemaphoreReleasedOnSnapshotError(t *testing.T) {
	t.Parallel()

	const giant = "giant-err"
	snapErr := errors.New("snapshot failed: injected test error")

	largeCh := make(chan SelectedRepository, 1)
	largeCh <- SelectedRepository{RepoPath: giant, RemoteURL: "https://github.com/example/repo"}
	close(largeCh)

	smallCh := make(chan SelectedRepository)
	close(smallCh)

	stream := make(chan CollectedGeneration, 1)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for gen := range stream {
			drainFactChannel(gen.Facts)
		}
	}()

	stub := &stubRepositorySnapshotter{
		snapshots: map[string]RepositorySnapshot{
			giant: {RepoPath: giant, FileCount: 9999},
		},
		errForRepoPath: map[string]error{
			giant: snapErr,
		},
	}

	src := &GitSource{Component: "collector-git", Snapshotter: stub}
	src.stream = stream

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		once      sync.Once
		firstErr  error
		completed atomic.Int64
	)
	sem := make(chan struct{}, 1)
	sc := &snapshotScheduler{
		source:      src,
		smallCh:     smallCh,
		largeCh:     largeCh,
		largeSem:    sem,
		workerCtx:   ctx,
		cancel:      cancel,
		sourceRunID: "run-err",
		observedAt:  time.Unix(0, 0).UTC(),
		errOnce:     &once,
		firstErr:    &firstErr,
		completed:   &completed,
	}

	sc.runLargePreferring(1)
	close(stream)
	<-drainDone

	// The error must have propagated.
	if firstErr == nil {
		t.Fatal("expected firstErr to be set after snapshot error, got nil")
	}
	if !errors.Is(firstErr, snapErr) {
		t.Fatalf("firstErr = %v, want to wrap %v", firstErr, snapErr)
	}

	// Semaphore must be fully released despite the error.
	if got := len(sem); got != 0 {
		t.Fatalf("largeSem has %d token(s) after snapshot error, want 0 (afterSnapshot not called on error path)", got)
	}
}
