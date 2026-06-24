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

// gatedConcurrencySnapshotter is a RepositorySnapshotter that deterministically
// measures the peak number of *giant* SnapshotRepository calls in flight at once,
// without relying on a sleep to coax overlap. Every gated (giant) path increments
// the in-flight counter, records the high-water mark, announces itself on
// arrived, and then blocks on release until the test opens the gate. Small repos
// are never gated and pass through instantly, so the peak reflects giant
// concurrency alone.
//
// The test's gate controller reads exactly semCap arrivals before closing
// release. At that instant semCap giants are past the in-flight increment and
// blocked on release with none yet decremented, so inflight==semCap and the
// recorded peak is pinned to semCap deterministically — there is no timing window
// in which the assertion can flake.
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

// TestSchedulerSemaphoreCapNotExceeded proves that largeSem caps concurrent
// giant snapshots at LargeRepoMaxConcurrent under the real multi-worker mix —
// min(semCap, workers) large-preferring lanes plus the rest small-preferring —
// all sharing one smallCh, largeCh, and buffered largeSem.
//
// Both lanes are pre-filled with several giants and many smalls and then closed
// (the worst-case "classifier already drained both lanes" ordering), and all
// four worker goroutines are spawned via the two role methods. A deterministic
// gate (the gatedConcurrencySnapshotter) blocks every giant in flight until
// exactly semCap of them are simultaneously executing, then opens. The recorded
// peak must equal semCap: never above (semaphore must hold) and never below (the
// gate pins semCap giants in flight, so a regression that serialized giants or
// failed to use both slots would be caught too). Every repo must also drain
// exactly once.
//
// It fails a regression where the semaphore is bypassed (peak > semCap) and a
// regression where giants cannot run concurrently up to the cap (peak < semCap).
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

// TestSchedulerSmallWorkerReservedWhenCapEqualsWorkers proves the P2 fix: when
// ESHU_SNAPSHOT_WORKERS <= ESHU_LARGE_REPO_MAX_CONCURRENT (e.g. both == 2),
// at least one worker must be small-preferring so small repos drain WITHOUT
// waiting for largeCh to close.
//
// Setup: largeCh is intentionally never closed (simulating a slow classifier
// or a long-running discovery goroutine). smallCh has one small repo and is
// closed. With workers==2 and semCap==2 (cap >= workers), the pre-fix code set
// largePreferring=min(2,2)=2, making BOTH workers large-preferring; those
// workers each reserve a sem slot and then block waiting for largeCh forever,
// so the small repo would never be processed. The fix clamps largePreferring to
// workers-1 == 1, leaving one worker small-preferring, which drains the small
// repo promptly.
//
// The test fails without the `if largePreferring >= workers && workers > 1`
// reserve clamp in git_source_stream.go and passes with it.
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
