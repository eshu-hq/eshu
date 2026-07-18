// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOrphanSweepStoreComputesSetDifferenceAndOrdersClearBeforeMark(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	// unmarked orphan -> should be marked.
	graph.seed("Repository", "orphan-1", false, nil)
	// marked orphan not yet aged -> stays marked, no write needed for it.
	graph.seed("Repository", "orphan-2", false, int64Ptr(950))
	// marked node that reconnected -> must be cleared.
	graph.seed("Repository", "relinked-1", true, int64Ptr(500))
	// connected, never marked -> untouched.
	graph.seed("Repository", "connected-1", true, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second, // cutoff = 900
		BatchLimit: 10,
		CountLimit: 100,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}

	if got := result.Counts["Repository"]; got != 2 {
		t.Fatalf("Repository orphan count = %d, want 2 (orphan-1, orphan-2)", got)
	}
	if got := result.Marked["Repository"]; got != 1 {
		t.Fatalf("Repository marked = %d, want 1 (orphan-1)", got)
	}
	if got := result.Deleted["Repository"]; got != 0 {
		t.Fatalf("Repository deleted = %d, want 0 (orphan-2 not aged)", got)
	}

	if len(graph.execs) != 2 {
		t.Fatalf("executor calls = %d, want 2 (clear, mark)", len(graph.execs))
	}
	if !strings.Contains(graph.execs[0].Cypher, "REMOVE n.eshu_orphan_observed_at_unix") {
		t.Fatalf("first executed statement must be clear, got: %s", graph.execs[0].Cypher)
	}
	if !strings.Contains(graph.execs[1].Cypher, "SET n.eshu_orphan_observed_at_unix") {
		t.Fatalf("second executed statement must be mark, got: %s", graph.execs[1].Cypher)
	}

	clearKeys, _ := graph.execs[0].Parameters["keys"].([]string)
	if len(clearKeys) != 1 || clearKeys[0] != "relinked-1" {
		t.Fatalf("clear keys = %#v, want [relinked-1]", clearKeys)
	}
	markKeys, _ := graph.execs[1].Parameters["keys"].([]string)
	if len(markKeys) != 1 || markKeys[0] != "orphan-1" {
		t.Fatalf("mark keys = %#v, want [orphan-1]", markKeys)
	}

	if n, ok := graph.node("Repository", "relinked-1"); !ok || n.observedAt != nil {
		t.Fatalf("relinked-1 marker = %v, want cleared", n)
	}
	if n, ok := graph.node("Repository", "orphan-1"); !ok || n.observedAt == nil {
		t.Fatalf("orphan-1 marker = %v, want set", n)
	}
	if n, ok := graph.node("Repository", "connected-1"); !ok || n.observedAt != nil {
		t.Fatalf("connected-1 marker = %v, want untouched (nil)", n)
	}
}

func TestOrphanSweepStoreDeletesAgedOrphansAndPreservesConnected(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("File", "orphan-aged", false, int64Ptr(500)) // aged: observedAt <= cutoff(900)
	graph.seed("File", "connected", true, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 10,
		CountLimit: 100,
		Labels:     []string{"File"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if got := result.Deleted["File"]; got != 1 {
		t.Fatalf("File deleted = %d, want 1", got)
	}
	if remaining := graph.remaining("File"); remaining != 1 {
		t.Fatalf("File remaining nodes = %d, want 1 (connected survives)", remaining)
	}
	if _, ok := graph.node("File", "connected"); !ok {
		t.Fatal("connected node was deleted, want preserved")
	}
	if _, ok := graph.node("File", "orphan-aged"); ok {
		t.Fatal("orphan-aged node survived sweep, want deleted")
	}
}

func TestOrphanSweepStoreBoundsMarkAndSweepToBatchLimit(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	for i := 0; i < 5; i++ {
		graph.seed("Module", fmt.Sprintf("unmarked-%d", i), false, nil)
	}
	for i := 0; i < 5; i++ {
		graph.seed("Module", fmt.Sprintf("aged-%d", i), false, int64Ptr(500))
	}

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 100,
		Labels:     []string{"Module"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if got := result.Counts["Module"]; got != 10 {
		t.Fatalf("Module orphan count = %d, want 10", got)
	}
	if got := result.Marked["Module"]; got != 2 {
		t.Fatalf("Module marked = %d, want bounded 2", got)
	}
	if got := result.Deleted["Module"]; got != 2 {
		t.Fatalf("Module deleted = %d, want bounded 2", got)
	}
	if remaining := graph.remaining("Module"); remaining != 8 {
		t.Fatalf("Module remaining = %d, want 8 (10 - 2 deleted)", remaining)
	}
}

func TestOrphanSweepStoreConvergesAcrossBoundedCyclesForAllDefaultLabels(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	seedCounts := map[string]int{
		"Repository":       1,
		"Platform":         0,
		"EvidenceArtifact": 0,
		"File":             3,
		"Directory":        2,
		"Module":           2,
	}
	for label, n := range seedCounts {
		for i := 0; i < n; i++ {
			graph.seed(label, fmt.Sprintf("%s-orphan-%d", label, i), false, int64Ptr(0))
		}
	}

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }
	policy := OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 10,
	}

	for cycle := 0; cycle < 5; cycle++ {
		result, err := store.SweepOrphanNodes(context.Background(), policy)
		if err != nil {
			t.Fatalf("SweepOrphanNodes() cycle %d error = %v", cycle, err)
		}
		if orphanSweepTestTotal(result.Deleted) == 0 && cycle > 0 {
			break
		}
	}

	for label := range seedCounts {
		if got := graph.remaining(label); got != 0 {
			t.Fatalf("%s remaining = %d, want converged to 0", label, got)
		}
	}
}

func TestOrphanSweepStoreUsesInjectedClockForMarkAndCutoff(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "orphan-1", false, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	_, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 10,
		CountLimit: 10,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if len(graph.execs) != 1 {
		t.Fatalf("executor calls = %d, want 1 (mark only)", len(graph.execs))
	}
	if got := graph.execs[0].Parameters["observed_at_unix"]; got != int64(1_000) {
		t.Fatalf("mark observed_at_unix = %#v, want injected clock 1000", got)
	}
}
