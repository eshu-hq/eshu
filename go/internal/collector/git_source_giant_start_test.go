// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
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
