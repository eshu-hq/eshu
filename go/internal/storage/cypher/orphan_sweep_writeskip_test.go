// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

// TestOrphanSweepStoreSteadyStateRunsTwoReadsZeroWrites proves the steady
// no-orphan state (a connected, never-marked candidate) issues exactly the S1
// + S2 reads and zero writes, with all three writes accounted as skipped.
func TestOrphanSweepStoreSteadyStateRunsTwoReadsZeroWrites(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "connected-1", true, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(graph.execs); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all writes should be skipped)", got)
	}
	if got := result.Counts["Repository"]; got != 0 {
		t.Fatalf("Repository count = %d, want 0", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3 (clear+mark+sweep all skipped)", got)
	}
}

// TestOrphanSweepStoreNoCandidatesSkipsS2ReadEntirely proves that when S1
// returns no candidates at all, the store never issues the S2 read (an even
// cheaper path than the connected-candidate steady state).
func TestOrphanSweepStoreNoCandidatesSkipsS2ReadEntirely(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	// No nodes seeded for Repository at all.

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(graph.execs); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
	if got := graph.s2Calls["Repository"]; got != 0 {
		t.Fatalf("S2 (connected-keys) reads = %d, want 0 when there are no candidates", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3", got)
	}
}

// TestOrphanSweepStoreRunsMarkOnlyWhenUnmarkedOrphansPresent proves mark
// executes alone when there is an unmarked orphan and nothing to clear or
// sweep.
func TestOrphanSweepStoreRunsMarkOnlyWhenUnmarkedOrphansPresent(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "orphan-1", false, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(graph.execs); got != 1 {
		t.Fatalf("executor calls = %d, want 1 (mark only)", got)
	}
	if got := result.Marked["Repository"]; got != 1 {
		t.Fatalf("Repository marked = %d, want 1", got)
	}
	if got := result.Deleted["Repository"]; got != 0 {
		t.Fatalf("Repository deleted = %d, want 0", got)
	}
	if got := result.Skipped["Repository"]; got != 2 {
		t.Fatalf("Repository skipped = %d, want 2 (clear+sweep skipped)", got)
	}
}

// TestOrphanSweepStoreRunsClearAndSweepWhenBothApply proves clear (a
// reconnected marked node) and sweep (an aged marked orphan) both execute in
// the same cycle while mark is skipped.
func TestOrphanSweepStoreRunsClearAndSweepWhenBothApply(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "relinked-1", true, int64Ptr(500))
	graph.seed("Repository", "aged-orphan-1", false, int64Ptr(500))

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(graph.execs); got != 2 {
		t.Fatalf("executor calls = %d, want 2 (clear, sweep)", got)
	}
	if got := result.Marked["Repository"]; got != 0 {
		t.Fatalf("Repository marked = %d, want 0 (mark skipped)", got)
	}
	if got := result.Deleted["Repository"]; got != 1 {
		t.Fatalf("Repository deleted = %d, want 1", got)
	}
	if got := result.Skipped["Repository"]; got != 1 {
		t.Fatalf("Repository skipped = %d, want 1 (mark skipped)", got)
	}
}

// TestOrphanSweepStoreSkipsClearWhenMarkedButNotRelinked reproduces the codex
// #4955 finding under the new anti-join design: a freshly marked orphan
// (marked, still disconnected, younger than the TTL) must not trigger a
// zero-row clear write.
func TestOrphanSweepStoreSkipsClearWhenMarkedButNotRelinked(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "marked-not-aged", false, int64Ptr(950)) // marked, still orphan, not aged

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second, // cutoff = 900
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := len(graph.execs); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (clear/mark/sweep all skipped)", got)
	}
	if got := result.Skipped["Repository"]; got != 3 {
		t.Fatalf("Repository skipped = %d, want 3", got)
	}
}
