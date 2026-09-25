// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestContentReaderRepositoryCoverageDerivesEntityTotalsFromOnePass pins the
// #7126 single-pass shape: one grouped content_entities statement yields the
// type counts, and the total and newest indexed_at are derived from its rows
// instead of two more whole-repository scans. It lives in package query (not
// internal/query/repository) because it drives the root ContentReader through
// the recording-DB rig, which repository/ tests cannot reach without an import
// cycle. The rig serves results strictly in order, so any extra content_entities
// statement would consume the wrong result and fail the assertions.
func TestContentReaderRepositoryCoverageDerivesEntityTotalsFromOnePass(t *testing.T) {
	t.Parallel()

	fileIndexedAt := time.Date(2026, 5, 29, 11, 58, 0, 0, time.UTC)
	older := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 5, 29, 12, 5, 0, 0, time.UTC)
	db := openContentReaderTestDB(t, repositoryCoverageResults(fileIndexedAt, [][]driver.Value{
		{"Function", int64(5), older},
		{"TerraformResource", int64(2), newest},
	}))

	coverage, err := NewContentReader(db).RepositoryCoverage(t.Context(), "repo-1")
	if err != nil {
		t.Fatalf("RepositoryCoverage() error = %v, want nil", err)
	}
	if got, want := coverage.EntityTypes, []querycontract.RepositoryEntityTypeCount{
		{EntityType: "Function", Count: 5},
		{EntityType: "TerraformResource", Count: 2},
	}; !repositoryEntityTypeCountsEqual(got, want) {
		t.Fatalf("EntityTypes = %#v, want %#v", got, want)
	}
	if got, want := coverage.EntityCount, 7; got != want {
		t.Fatalf("EntityCount = %d, want %d (sum of type counts)", got, want)
	}
	if !coverage.EntityIndexedAt.Equal(newest) || coverage.EntityIndexedAt.Location() != time.UTC {
		t.Fatalf("EntityIndexedAt = %v, want newest group max %v in UTC", coverage.EntityIndexedAt, newest)
	}
	if coverage.FileCount != 42 || !coverage.FileIndexedAt.Equal(fileIndexedAt) {
		t.Fatalf("file coverage = %d @ %v, want 42 @ %v", coverage.FileCount, coverage.FileIndexedAt, fileIndexedAt)
	}
}

// TestContentReaderRepositoryCoverageEmptyAndNullEntityRows preserves the
// pre-#7126 values for a repository with no entities and for groups whose
// indexed_at is NULL: a zero count, a zero time, and a non-nil empty slice; a
// NULL group max never masks a real one.
func TestContentReaderRepositoryCoverageEmptyAndNullEntityRows(t *testing.T) {
	t.Parallel()

	fileIndexedAt := time.Date(2026, 5, 29, 11, 58, 0, 0, time.UTC)
	real := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	empty, err := NewContentReader(openContentReaderTestDB(t, repositoryCoverageResults(fileIndexedAt, nil))).
		RepositoryCoverage(t.Context(), "repo-empty")
	if err != nil {
		t.Fatalf("RepositoryCoverage(empty) error = %v", err)
	}
	if empty.EntityCount != 0 || !empty.EntityIndexedAt.IsZero() || empty.EntityTypes == nil || len(empty.EntityTypes) != 0 {
		t.Fatalf("empty coverage = %d %v %#v, want 0, zero time, non-nil empty slice",
			empty.EntityCount, empty.EntityIndexedAt, empty.EntityTypes)
	}

	nullOnly, err := NewContentReader(openContentReaderTestDB(t, repositoryCoverageResults(fileIndexedAt, [][]driver.Value{
		{"Function", int64(3), nil},
	}))).RepositoryCoverage(t.Context(), "repo-null")
	if err != nil {
		t.Fatalf("RepositoryCoverage(null) error = %v", err)
	}
	if nullOnly.EntityCount != 3 || !nullOnly.EntityIndexedAt.IsZero() {
		t.Fatalf("null-only coverage = %d %v, want 3 with zero time", nullOnly.EntityCount, nullOnly.EntityIndexedAt)
	}

	mixed, err := NewContentReader(openContentReaderTestDB(t, repositoryCoverageResults(fileIndexedAt, [][]driver.Value{
		{"Function", int64(3), nil},
		{"Module", int64(1), real},
	}))).RepositoryCoverage(t.Context(), "repo-mixed")
	if err != nil {
		t.Fatalf("RepositoryCoverage(mixed) error = %v", err)
	}
	if !mixed.EntityIndexedAt.Equal(real) {
		t.Fatalf("mixed EntityIndexedAt = %v, want %v (NULL group max must not mask a real one)", mixed.EntityIndexedAt, real)
	}
}

// repositoryCoverageResults queues the four statements RepositoryCoverage
// issues, in order: file count, file indexed_at, language distribution, and the
// single grouped content_entities pass answered with entityRows.
func repositoryCoverageResults(fileIndexedAt time.Time, entityRows [][]driver.Value) []contentReaderQueryResult {
	return []contentReaderQueryResult{
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(42)}}},
		{columns: []string{"indexed_at"}, rows: [][]driver.Value{{fileIndexedAt}}},
		{
			columns: []string{"language", "file_count"},
			rows:    [][]driver.Value{{"go", int64(30)}, {"yaml", int64(12)}},
		},
		{
			columns: []string{"entity_type", "entity_count", "indexed_at"},
			rows:    entityRows,
			queryContains: []string{
				"count(*)", "max(indexed_at)", "FROM content_entities", "GROUP BY entity_type",
				"ORDER BY entity_count DESC, entity_type",
			},
		},
	}
}

func repositoryEntityTypeCountsEqual(got, want []querycontract.RepositoryEntityTypeCount) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
