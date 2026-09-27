// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/jackc/pgx/v5"
)

// A one-repository page must bypass pgx's named statement cache: PostgreSQL
// otherwise chooses a generic plan after mixed search patterns and may scan
// the global trigram index for a one-character query.
func TestSearchEntitiesSingleRepoUsesUnpreparedPlanAndKeepsPage(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
		rows: [][]driver.Value{{
			"entity-2", "repo-1", "b.go", "Function", "second",
			int64(2), int64(3), "go", "a", []byte(`{}`),
		}},
		queryContainsInOrder: []string{
			"WHERE repo_id = $1 AND source_cache ILIKE '%' || $2 || '%'",
			"ORDER BY repo_id, relative_path, start_line, entity_id",
			"LIMIT $3 OFFSET $4",
		},
		wantArgs: []driver.Value{pgx.QueryExecModeExec, "repo-1", "a", int64(2), int64(1)},
	}})

	rows, err := NewContentReader(db).SearchEntities(context.Background(), "repo-1", nil, "a", 2, 1)
	if err != nil {
		t.Fatalf("SearchEntities(): %v", err)
	}
	if len(rows) != 1 || rows[0].EntityID != "entity-2" {
		t.Fatalf("SearchEntities() = %+v, want the second ordered row", rows)
	}
}
