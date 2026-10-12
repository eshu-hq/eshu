// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"testing"
)

// TestSelectHydrationIndexesAtTheFloorKeepsTheOldSelection pins that the
// allowance HydrationFloor selects exactly what the 50-candidate cap selected
// before #7951, so every allowance at or above the floor is a superset.
func TestSelectHydrationIndexesAtTheFloorKeepsTheOldSelection(t *testing.T) {
	t.Parallel()

	files := make([]File, 0, HydrationFloor+10)
	for i := 0; i < HydrationFloor+10; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%03d.yaml", i), ""))
	}
	// A candidate that already carries content does not use a slot.
	files[3].Content = "k: v\n"

	indexes, candidates := SelectHydrationIndexes(files, isConfigArtifactCandidate, fileContent, HydrationFloor)
	if candidates != HydrationFloor+9 {
		t.Fatalf("candidates = %d, want %d", candidates, HydrationFloor+9)
	}
	if len(indexes) != HydrationFloor {
		t.Fatalf("selected %d files, want the floor %d", len(indexes), HydrationFloor)
	}
	for _, index := range indexes {
		if index == 3 {
			t.Fatal("selected a file that already carries content")
		}
	}
	if indexes[0] != 0 || indexes[len(indexes)-1] != HydrationFloor {
		t.Fatalf("selection = %d..%d, want list order from 0 to %d", indexes[0], indexes[len(indexes)-1], HydrationFloor)
	}
}
