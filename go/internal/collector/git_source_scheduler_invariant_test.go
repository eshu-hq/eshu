package collector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedConcurrencySnapshotter is a RepositorySnapshotter that deterministically
// measures peak concurrent giant SnapshotRepository calls without relying on
// sleeps. Every gated (giant) path increments the in-flight counter, records
// the high-water mark, signals arrived, then blocks on release. Small repos pass
// through instantly. The gate controller closes release after exactly semCap
// arrivals, pinning inflight==semCap with none yet decremented — no flake window.
type gatedConcurrencySnapshotter struct {
	snapshots map[string]RepositorySnapshot
	gate      map[string]bool // repo paths that block on release (the giants)
	arrived   chan string     // one send per gated snapshot when it goes in flight
	release   chan struct{}   // closed by the test to let gated snapshots complete

	mu       sync.Mutex
	inflight int
	peak     int
	calls    []string
}

// SnapshotRepository satisfies RepositorySnapshotter. Gated repos block on the
// release gate (or ctx) while in flight; ungated repos return immediately.
func (g *gatedConcurrencySnapshotter) SnapshotRepository(
	ctx context.Context, repo SelectedRepository,
) (RepositorySnapshot, error) {
	g.mu.Lock()
	g.calls = append(g.calls, repo.RepoPath)
	gated := g.gate[repo.RepoPath]
	if gated {
		g.inflight++
		if g.inflight > g.peak {
			g.peak = g.inflight
		}
	}
	g.mu.Unlock()

	if gated {
		g.arrived <- repo.RepoPath
		select {
		case <-g.release:
		case <-ctx.Done():
		}
		g.mu.Lock()
		g.inflight--
		g.mu.Unlock()
	}

	return g.snapshots[repo.RepoPath], nil
}

// TestSchedulerSemaphoreCapNotExceeded proves largeSem caps concurrent giant
// snapshots at semCap under the real multi-worker mix. Both lanes are pre-filled
// and closed (worst-case ordering). gatedConcurrencySnapshotter blocks every
// giant until exactly semCap are in flight simultaneously, then opens; the
// recorded peak must equal semCap exactly — above means the semaphore is
// bypassed, below means giants cannot run concurrently up to the cap.
func TestSchedulerSemaphoreCapNotExceeded(t *testing.T) {
	t.Parallel()

	const (
		workers = 4
		semCap  = 2 // LargeRepoMaxConcurrent
	)
	giants := []string{"giant-0", "giant-1", "giant-2", "giant-3"}
	smalls := []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7"}
	total := len(giants) + len(smalls)

	snapshots := make(map[string]RepositorySnapshot, total)
	gate := make(map[string]bool, len(giants))
	for _, g := range giants {
		snapshots[g] = RepositorySnapshot{RepoPath: g, FileCount: 9999}
		gate[g] = true
	}
	for _, s := range smalls {
		snapshots[s] = RepositorySnapshot{RepoPath: s, FileCount: 0}
	}

	snap := &gatedConcurrencySnapshotter{
		snapshots: snapshots,
		gate:      gate,
		arrived:   make(chan string, len(giants)),
		release:   make(chan struct{}),
	}

	// Pre-fill BOTH lanes fully, then close, so all workers drain everything and
	// exit. Buffered to the lane size so the fills never block.
	largeCh := make(chan SelectedRepository, len(giants))
	for _, g := range giants {
		largeCh <- SelectedRepository{RepoPath: g, RemoteURL: "https://github.com/example/repo"}
	}
	close(largeCh)
	smallCh := make(chan SelectedRepository, len(smalls))
	for _, s := range smalls {
		smallCh <- SelectedRepository{RepoPath: s, RemoteURL: "https://github.com/example/repo"}
	}
	close(smallCh)

	stream := make(chan CollectedGeneration, total)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for gen := range stream {
			drainFactChannel(gen.Facts)
		}
	}()

	src := &GitSource{Component: "collector-git", Snapshotter: snap}
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

	// Deterministic gate controller: wait until exactly semCap giants are in
	// flight (which pins inflight==semCap with none yet released), then open the
	// gate. This makes the peak deterministic instead of relying on a sleep to
	// produce overlap. semCap arrivals are guaranteed: semCap large-preferring
	// workers each reserve a slot and block-pull a giant, and len(giants) >= semCap.
	gateOpened := make(chan struct{})
	go func() {
		defer close(gateOpened)
		for range semCap {
			<-snap.arrived
		}
		close(snap.release)
	}()

	// Spawn the real worker mix: min(semCap, workers) large-preferring lanes and
	// the rest small-preferring, all sharing the scheduler's channels and sem.
	largeWorkers := semCap
	if workers < semCap {
		largeWorkers = workers
	}
	var wg sync.WaitGroup
	for i := range largeWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sc.runLargePreferring(id)
		}(i + 1)
	}
	for i := range workers - largeWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sc.runSmallPreferring(id)
		}(largeWorkers + i + 1)
	}

	wg.Wait()
	<-gateOpened
	close(stream)
	<-drainDone

	if firstErr != nil {
		t.Fatalf("unexpected worker error: %v", firstErr)
	}

	snap.mu.Lock()
	gotCalls := len(snap.calls)
	peakConcurrent := snap.peak
	snap.mu.Unlock()

	if gotCalls != total {
		t.Fatalf("processed %d repos, want %d (every repo must drain exactly once)", gotCalls, total)
	}
	if peakConcurrent > semCap {
		t.Fatalf("peak concurrent giant snapshots = %d, exceeded semaphore cap %d (semaphore not enforced)",
			peakConcurrent, semCap)
	}
	if peakConcurrent != semCap {
		t.Fatalf("peak concurrent giant snapshots = %d, want exactly %d (gate pins semCap giants in flight)",
			peakConcurrent, semCap)
	}
}

// TestSchedulerNoSemaphoreLeakOnCtxCancel proves that cancelling the worker
// context mid-flight does not leave the large-repo semaphore permanently
// occupied. A leak would cause the next slot acquire to block forever; the
// test enforces a 2 s deadline on worker exit and checks len(sem)==0 after.
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
// does not leave the large-repo semaphore occupied. If afterSnapshot is not
// called on the error path the slot is never returned and subsequent acquires
// deadlock; the test checks len(sem)==0 after the error propagates.
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

// TestSchedulerSmallWorkerReservedWhenCapEqualsWorkers proves the P2 fix: when
// ESHU_SNAPSHOT_WORKERS <= ESHU_LARGE_REPO_MAX_CONCURRENT both workers would
// become large-preferring and block forever on largeCh, starving small repos.
// The fix clamps largePreferring to workers-1 so one small-preferring worker
// always exists. largeCh is never closed; the test asserts the single small
// repo drains within 3 s via the reserved small-preferring worker.
func TestSchedulerSmallWorkerReservedWhenCapEqualsWorkers(t *testing.T) {
	t.Parallel()

	const (
		workers = 2
		semCap  = 2 // cap >= workers: the P2 starvation condition
	)

	const small = "small-repo"

	snapshots := map[string]RepositorySnapshot{
		small: {RepoPath: small, FileCount: 0},
	}
	stub := &stubRepositorySnapshotter{snapshots: snapshots}

	stream := make(chan CollectedGeneration, workers+1)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for gen := range stream {
			drainFactChannel(gen.Facts)
		}
	}()

	src := &GitSource{Component: "collector-git", Snapshotter: stub}
	src.stream = stream

	// smallCh: one small repo then closed — the test asserts this drains promptly.
	smallCh := make(chan SelectedRepository, 1)
	smallCh <- SelectedRepository{RepoPath: small, RemoteURL: "https://github.com/example/repo"}
	close(smallCh)

	// largeCh: never closed and never written to. If both workers become
	// large-preferring, they both block here forever and the small repo starves.
	largeCh := make(chan SelectedRepository)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		sourceRunID: "run-reserve",
		observedAt:  time.Unix(0, 0).UTC(),
		errOnce:     &once,
		firstErr:    &firstErr,
		completed:   &completed,
	}

	// Spawn the same worker mix that startStream would use after the P2 fix:
	// largePreferring = min(semCap, workers) - 1 = 1, so one large-preferring
	// and one small-preferring. The small-preferring worker drains the small
	// repo and exits; the large-preferring worker eventually gets ctx-cancelled.
	largePreferring := semCap
	if largePreferring > workers {
		largePreferring = workers
	}
	if largePreferring >= workers && workers > 1 {
		largePreferring = workers - 1
	}

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx < largePreferring {
				sc.runLargePreferring(idx + 1)
			} else {
				sc.runSmallPreferring(idx + 1)
			}
		}(i)
	}

	// The small repo must be processed well before the timeout. We detect
	// completion by polling completed until it reaches 1, then cancel to
	// unblock any large-preferring workers blocked on largeCh.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(1 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if completed.Load() >= 1 {
				cancel() // unblock the large-preferring worker
				goto done
			}
		case <-deadline.C:
			cancel()
			t.Fatal("small repo was not processed within 3s: small-preferring worker not reserved (P2 starvation)")
		}
	}
done:
	close(largeCh) // unblock runSmallPreferring's drainLarge which ranges over largeCh
	wg.Wait()
	close(stream)
	<-drainDone

	if firstErr != nil {
		t.Fatalf("unexpected worker error: %v", firstErr)
	}

	stub.mu.Lock()
	calls := stub.calls
	stub.mu.Unlock()

	if len(calls) != 1 || calls[0] != small {
		t.Fatalf("processed repos = %v, want [%q]", calls, small)
	}
}
