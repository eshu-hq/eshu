// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

func TestSearchCodeCandidatesDefaultsNonPositiveLimit(t *testing.T) {
	t.Parallel()

	entities := make([]querycontract.EntityContent, 51)
	for i := range entities {
		entities[i] = querycontract.EntityContent{
			EntityID:     fmt.Sprintf("entity-%03d", i),
			RepoID:       "repo-1",
			RelativePath: fmt.Sprintf("file-%03d.go", i),
			EntityName:   "decode",
			SourceCache:  "func decode() {}",
		}
	}
	store := content.FakePortContentStore{Entities: entities}
	for _, limit := range []int{0, -1} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			t.Parallel()
			names, sources, err := store.SearchCodeCandidates(t.Context(), "repo-1", "decode", "", limit, false)
			if err != nil {
				t.Fatalf("SearchCodeCandidates() error = %v", err)
			}
			if len(names) != 50 || len(sources) != 50 {
				t.Fatalf("SearchCodeCandidates() rows = %d names, %d sources; want 50 each", len(names), len(sources))
			}
			if names[49].EntityID != "entity-049" || sources[49].EntityID != "entity-049" {
				t.Fatalf("SearchCodeCandidates() last rows = %q, %q; want entity-049", names[49].EntityID, sources[49].EntityID)
			}
		})
	}
}
