// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestContentReaderRepositoryCoverageIncludesEntityTypeCounts pins the
// ContentReader.RepositoryCoverage SQL for entity-type fan-out. It lives in
// package query (not internal/query/repository) because it drives the root
// ContentReader through the recording-DB rig, which repository/ tests cannot
// reach without an import cycle.
func TestContentReaderRepositoryCoverageIncludesEntityTypeCounts(t *testing.T) {
	t.Parallel()

	fileIndexedAt := time.Date(2026, 5, 29, 11, 58, 0, 0, time.UTC)
	entityIndexedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(42)}}},
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(7)}}},
		{columns: []string{"indexed_at"}, rows: [][]driver.Value{{fileIndexedAt}}},
		{columns: []string{"indexed_at"}, rows: [][]driver.Value{{entityIndexedAt}}},
		{
			columns: []string{"language", "file_count"},
			rows: [][]driver.Value{
				{"go", int64(30)},
				{"yaml", int64(12)},
			},
		},
		{
			columns: []string{"entity_type", "entity_count"},
			rows: [][]driver.Value{
				{"Function", int64(5)},
				{"TerraformResource", int64(2)},
			},
			queryContains: []string{"FROM content_entities", "GROUP BY entity_type", "ORDER BY entity_count DESC, entity_type"},
		},
	})

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
