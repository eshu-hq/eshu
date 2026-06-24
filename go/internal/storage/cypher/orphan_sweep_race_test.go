// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

func TestOrphanSweepStoreDelaysCodeStructureDeletionDuringProjectionRace(t *testing.T) {
	t.Parallel()

	graph := &convergingOrphanSweepGraph{
		orphanCounts: map[string]int64{
			"File":      1,
			"Directory": 1,
			"Module":    1,
		},
		agedCounts: map[string]int64{
			"File":      0,
			"Directory": 0,
			"Module":    0,
		},
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
