// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

// TestOrphanSweepStoreDelaysCodeStructureDeletionDuringProjectionRace proves
// a node observed disconnected for the first time is marked, not deleted,
// even though it is technically an orphan this cycle -- deletion waits for
// the TTL to age past the observation.
func TestOrphanSweepStoreDelaysCodeStructureDeletionDuringProjectionRace(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	for _, label := range []string{"File", "Directory", "Module"} {
		graph.seed(label, label+"-racing", false, nil)
	}

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 5,
		CountLimit: 5,
		Labels:     []string{"File", "Directory", "Module"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}

	for _, label := range []string{"File", "Directory", "Module"} {
		if got := result.Counts[label]; got != 1 {
			t.Fatalf("%s count = %d, want newly observed orphan during projection race", label, got)
		}
		if got := result.Marked[label]; got != 1 {
			t.Fatalf("%s marked = %d, want observation marker before any deletion", label, got)
		}
		if got := result.Deleted[label]; got != 0 {
			t.Fatalf("%s deleted = %d, want no deletion before TTL-aged observation", label, got)
		}
		if got := graph.remaining(label); got != 1 {
			t.Fatalf("%s remaining = %d, want temporarily edge-less node retained", label, got)
		}
	}
}

// TestOrphanSweepStoreTOCTOUGuardDropsReconnectedKeyBeforeSweep proves the
// guard immediately before S5: a node that regains a relationship between the
// top-of-cycle S2 read and the sweep delete must not be deleted, even though
// it was aged, marked, and orphaned at the top of the cycle.
func TestOrphanSweepStoreTOCTOUGuardDropsReconnectedKeyBeforeSweep(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("File", "reconnects-mid-cycle", false, int64Ptr(500)) // aged orphan at top of cycle
	// After the first S2 (connected-keys) read for File returns (the
	// top-of-cycle connectivity check), flip this key connected -- simulating
	// a relationship written by a concurrent projector between the
	// top-of-cycle read and the TOCTOU re-verify immediately before delete.
	graph.flipAfterCall("File", 1, "reconnects-mid-cycle")

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 5,
		CountLimit: 5,
		Labels:     []string{"File"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}

	if got := graph.s2Calls["File"]; got != 2 {
		t.Fatalf("S2 (connected-keys) reads = %d, want 2 (top-of-cycle + TOCTOU re-verify)", got)
	}
	if got := result.Deleted["File"]; got != 0 {
		t.Fatalf("File deleted = %d, want 0 (TOCTOU guard must drop the reconnected key)", got)
	}
	if _, ok := graph.node("File", "reconnects-mid-cycle"); !ok {
		t.Fatal("reconnects-mid-cycle was deleted despite reconnecting before the TOCTOU re-verify")
	}
}

// TestOrphanSweepStoreTOCTOUGuardStillSweepsKeysThatStayDisconnected proves
// the TOCTOU re-verify does not block a normal sweep when nothing reconnects.
func TestOrphanSweepStoreTOCTOUGuardStillSweepsKeysThatStayDisconnected(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("File", "stays-disconnected", false, int64Ptr(500))

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 5,
		CountLimit: 5,
		Labels:     []string{"File"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := graph.s2Calls["File"]; got != 2 {
		t.Fatalf("S2 (connected-keys) reads = %d, want 2 (top-of-cycle + TOCTOU re-verify)", got)
	}
	if got := result.Deleted["File"]; got != 1 {
		t.Fatalf("File deleted = %d, want 1", got)
	}
	if _, ok := graph.node("File", "stays-disconnected"); ok {
		t.Fatal("stays-disconnected survived sweep, want deleted")
	}
}
