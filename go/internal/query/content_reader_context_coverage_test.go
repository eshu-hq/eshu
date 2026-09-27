// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestContentReaderRepositoryContextCoverageUsesOnlyFileGroups(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		rows      [][]driver.Value
		wantCount int
		want      []querycontract.RepositoryLanguageCount
	}{
		{
			name:      "files with null language bucket",
			rows:      [][]driver.Value{{"go", int64(2)}, {"unknown", int64(1)}},
			wantCount: 3,
			want: []querycontract.RepositoryLanguageCount{
				{Language: "go", FileCount: 2}, {Language: "unknown", FileCount: 1},
			},
		},
		{name: "empty repository", want: []querycontract.RepositoryLanguageCount{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := openContentReaderTestDB(t, []contentReaderQueryResult{{
				columns: []string{"language", "file_count"},
				rows:    tc.rows,
				queryContainsInOrder: []string{
					"SELECT coalesce(language, 'unknown')", "FROM content_files", "WHERE repo_id = $1", "GROUP BY language", "ORDER BY file_count DESC",
				},
				wantArgs: []driver.Value{"repo-1"},
			}})
			coverage, err := NewContentReader(db).RepositoryContextCoverage(t.Context(), "repo-1")
			if err != nil {
				t.Fatalf("RepositoryContextCoverage() error = %v", err)
			}
			if !coverage.Available || coverage.FileCount != tc.wantCount || !reflect.DeepEqual(coverage.Languages, tc.want) {
				t.Fatalf("coverage = %#v, want file count %d and languages %#v", coverage, tc.wantCount, tc.want)
			}
			if coverage.EntityCount != 0 || len(coverage.EntityTypes) != 0 || !coverage.EntityIndexedAt.IsZero() {
				t.Fatalf("context coverage populated entity fields: %#v", coverage)
			}
		})
	}
}
