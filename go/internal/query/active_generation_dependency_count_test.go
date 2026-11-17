// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

// TestRepositoryDependencyCountReadsOnlyActiveGeneration pins the active
// relationship generation join (#10824). resolved_relationships is append-only,
// so a superseded or retired generation's rows stay on disk; only the
// relationship_generations status says which rows are current truth. The
// scripted driver cannot prove the filtering itself (the live proof does); it
// pins that the shipped SQL carries the same join and predicate the other
// resolved_relationships readers use, so a refactor cannot silently drop it.
func TestRepositoryDependencyCountReadsOnlyActiveGeneration(t *testing.T) {
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"count"},
		rows:    [][]driver.Value{{int64(3)}},
		queryContains: []string{
			"FROM resolved_relationships AS r",
			"JOIN relationship_generations AS g",
			"ON g.generation_id = r.generation_id",
			"WHERE g.status = 'active'",
			"AND r.source_repo_id = $1",
		},
		wantArgs: []driver.Value{"repo-one"},
	}})
	got, err := NewContentReader(db).repositoryDependencyCount(t.Context(), "repo-one")
	if err != nil || got != 3 {
		t.Fatalf("repositoryDependencyCount = %d, error = %v; want 3, nil", got, err)
	}
}
