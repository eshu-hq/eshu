// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestSelectHydrationIndexesCapsAtTheHydrationLimitInListOrder(t *testing.T) {
	t.Parallel()

	files := make([]querycontract.FileContent, 0, RepositoryArtifactHydrationLimit+10)
	for i := 0; i < RepositoryArtifactHydrationLimit+10; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%03d.yaml", i), ""))
	}
	// A candidate that already carries content does not use a slot.
	files[3].Content = "k: v\n"

	indexes := selectHydrationIndexes(files, isConfigArtifactCandidate)
	if len(indexes) != RepositoryArtifactHydrationLimit {
		t.Fatalf("selected %d files, want the cap %d", len(indexes), RepositoryArtifactHydrationLimit)
	}
	for _, index := range indexes {
		if index == 3 {
			t.Fatal("selected a file that already carries content")
		}
	}
	if indexes[0] != 0 || indexes[len(indexes)-1] != RepositoryArtifactHydrationLimit {
		t.Fatalf("selection = %d..%d, want list order from 0 to %d", indexes[0], indexes[len(indexes)-1], RepositoryArtifactHydrationLimit)
	}
}
