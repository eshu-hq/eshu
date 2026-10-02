// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
	"time"
)

// TestContentReaderRepositoryFilesLastIndexedAtReadsOnlyContentFiles pins the
// #7525 narrow read: exactly one content_files max(indexed_at) statement, bound
// to the repository, with no content_entities statement. The rig serves results
// strictly in order, so a second statement would fail with no result queued.
func TestContentReaderRepositoryFilesLastIndexedAtReadsOnlyContentFiles(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 29, 11, 58, 0, 0, time.FixedZone("offset", 3600))
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:              []string{"indexed_at"},
		rows:                 [][]driver.Value{{indexedAt}},
		queryContainsInOrder: []string{"SELECT max(indexed_at)", "FROM content_files", "WHERE repo_id = $1"},
		wantArgs:             []driver.Value{"repo-1"},
	}})

	got, err := NewContentReader(db).RepositoryFilesLastIndexedAt(t.Context(), "repo-1")
	if err != nil {
		t.Fatalf("RepositoryFilesLastIndexedAt() error = %v", err)
	}
	if !got.Equal(indexedAt) || got.Location() != time.UTC {
		t.Fatalf("RepositoryFilesLastIndexedAt() = %v, want %v in UTC", got, indexedAt)
	}
}

func TestContentReaderRepositoryFilesLastIndexedAtEmptyRepositoryIsZero(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"indexed_at"},
		rows:    [][]driver.Value{{nil}},
	}})
	got, err := NewContentReader(db).RepositoryFilesLastIndexedAt(t.Context(), "repo-empty")
	if err != nil || !got.IsZero() {
		t.Fatalf("RepositoryFilesLastIndexedAt(empty) = %v, %v, want zero time and nil error", got, err)
	}
}
