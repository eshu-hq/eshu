// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

func TestGraphOrphanNodeCountsUsesDefaultCodeStructureLabels(t *testing.T) {
	t.Parallel()

	graph := &convergingOrphanSweepGraph{
		orphanCounts: map[string]int64{
			"Repository":       1,
			"Platform":         2,
			"EvidenceArtifact": 3,
			"File":             4,
			"Directory":        5,
			"Module":           6,
		},
		agedCounts: map[string]int64{},
	}
	store := NewOrphanSweepStore(graph, graph)

	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts() error = %v, want nil", err)
	}

	for _, tc := range []struct {
		label string
		want  int64
	}{
		{label: "Repository", want: 1},
		{label: "Platform", want: 2},
		{label: "EvidenceArtifact", want: 3},
		{label: "File", want: 4},
		{label: "Directory", want: 5},
		{label: "Module", want: 6},
	} {
		if got := counts[tc.label]; got != tc.want {
			t.Fatalf("%s orphan count = %d, want %d in telemetry observer count set", tc.label, got, tc.want)
		}
	}
}
