// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func TestGraphOrphanNodeCountsUsesDefaultCodeStructureLabels(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	orphanCounts := map[string]int{
		"Repository":       1,
		"Platform":         2,
		"EvidenceArtifact": 3,
		"File":             4,
		"Directory":        5,
		"Module":           6,
	}
	for label, n := range orphanCounts {
		for i := 0; i < n; i++ {
			graph.seed(label, fmt.Sprintf("%s-orphan-%d", label, i), false, nil)
		}
		graph.seed(label, label+"-connected", true, nil)
	}

	store := NewOrphanSweepStore(graph, graph)

	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts() error = %v, want nil", err)
	}

	for label, want := range orphanCounts {
		if got := counts[label]; got != int64(want) {
			t.Fatalf("%s orphan count = %d, want %d (connected node excluded)", label, got, want)
		}
	}
}

func TestGraphOrphanNodeCountsExcludesConnectedNodes(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "orphan-1", false, nil)
	graph.seed("Repository", "orphan-2", false, nil)
	graph.seed("Repository", "connected-1", true, nil)

	store := NewOrphanSweepStore(graph, graph)
	store.Labels = []OrphanSweepLabel{OrphanSweepLabelRepository}

	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts() error = %v, want nil", err)
	}
	if got := counts["Repository"]; got != 2 {
		t.Fatalf("Repository orphan count = %d, want 2", got)
	}
}

func TestGraphOrphanNodeCountsReturnsZeroWithoutS2ReadWhenNoCandidates(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	store := NewOrphanSweepStore(graph, graph)
	store.Labels = []OrphanSweepLabel{OrphanSweepLabelRepository}

	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts() error = %v, want nil", err)
	}
	if got := counts["Repository"]; got != 0 {
		t.Fatalf("Repository orphan count = %d, want 0", got)
	}
	if got := graph.s2Calls["Repository"]; got != 0 {
		t.Fatalf("S2 reads = %d, want 0 when there are no candidates", got)
	}
}
