// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

func TestContentReaderSearchEntitiesOrdersTiedPageRowsByID(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:       []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata"},
		queryContains: []string{"ORDER BY repo_id, relative_path, start_line, entity_id"},
	}})
	reader := NewContentReader(db)
	if _, err := reader.SearchEntities(context.Background(), "repo-1", nil, "a", 2, 1); err != nil {
		t.Fatalf("search tied page: %v", err)
	}
}
