// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"testing"
)

func TestSelectHydrationIndexesCapsAtTheHydrationLimitInListOrder(t *testing.T) {
	t.Parallel()

	files := make([]File, 0, HydrationLimit+10)
	for i := 0; i < HydrationLimit+10; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%03d.yaml", i), ""))
	}
	// A candidate that already carries content does not use a slot.
	files[3].Content = "k: v\n"

	indexes := SelectHydrationIndexes(files, isConfigArtifactCandidate, fileContent)
	if len(indexes) != HydrationLimit {
		t.Fatalf("selected %d files, want the cap %d", len(indexes), HydrationLimit)
	}
	for _, index := range indexes {
		if index == 3 {
			t.Fatal("selected a file that already carries content")
		}
	}
	if indexes[0] != 0 || indexes[len(indexes)-1] != HydrationLimit {
		t.Fatalf("selection = %d..%d, want list order from 0 to %d", indexes[0], indexes[len(indexes)-1], HydrationLimit)
	}
}
