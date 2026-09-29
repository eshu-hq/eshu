// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestRepositoryLanguageInventoryTwoStageLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO content_files
		    (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
		VALUES
		    ('repo-a', 'a.go', '', 'h', 1, 'go', '2026-09-28T12:00:00Z'),
		    ('repo-a', 'b.go', '', 'h', 1, 'go', '2026-09-28T12:00:00Z'),
		    ('repo-a', 'a.py', '', 'h', 1, 'python', '2026-09-28T12:00:00Z'),
		    ('repo-a', 'a.none', '', 'h', 1, NULL, '2026-09-28T12:00:00Z'),
		    ('repo-a', 'a.empty', '', 'h', 1, '', '2026-09-28T12:00:00Z'),
		    ('repo-b', 'b.go', '', 'h', 1, 'go', '2026-09-28T12:00:00Z'),
		    ('repo-b', 'b.py', '', 'h', 1, 'python', '2026-09-28T12:00:00Z'),
		    ('repo-b', 'b.none', '', 'h', 1, NULL, '2026-09-28T14:00:00Z'),
		    ('repo-c', 'c.go', '', 'h', 1, 'go', '2026-09-28T13:00:00Z'),
		    ('repo-c', 'c.py', '', 'h', 1, 'python', '2026-09-28T12:00:00Z'),
		    ('repo-c', 'c.empty', '', 'h', 1, '', '2026-09-28T12:00:00Z')
	`)
	if err != nil {
		t.Fatalf("seed content_files: %v", err)
	}

	reader := NewContentReader(db)
	type wantRow struct {
		language        string
		repositoryCount int
		fileCount       int
		indexedHour     int
	}
	for _, tc := range []struct {
		name         string
		limit        int
		offset       int
		allScopes    bool
		repositories []string
		scopes       []string
		want         []wantRow
	}{
		{
			name:  "global tie order and normalized unknown",
			limit: 10, allScopes: true,
			want: []wantRow{{"go", 3, 4, 13}, {"unknown", 3, 4, 14}, {"python", 3, 3, 12}},
		},
		{
			name:  "overlapping grants do not double count",
			limit: 10, repositories: []string{"repo-a"}, scopes: []string{"repo-a", "repo-b"},
			want: []wantRow{{"go", 2, 3, 12}, {"unknown", 2, 3, 14}, {"python", 2, 2, 12}},
		},
		{
			name:  "bounded first page",
			limit: 2, allScopes: true,
			want: []wantRow{{"go", 3, 4, 13}, {"unknown", 3, 4, 14}},
		},
		{
			name:  "bounded offset",
			limit: 2, offset: 2, allScopes: true,
			want: []wantRow{{"python", 3, 3, 12}},
		},
		{name: "high offset", limit: 2, offset: 10, allScopes: true},
		{name: "empty grant", limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reader.RepositoryLanguageInventory(
				ctx, tc.limit, tc.offset, tc.allScopes, tc.repositories, tc.scopes,
			)
			if err != nil {
				t.Fatalf("RepositoryLanguageInventory(): %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %#v, want %d rows", got, len(tc.want))
			}
			for i, row := range got {
				want := tc.want[i]
				if row.Language != want.language || row.RepositoryCount != want.repositoryCount ||
					row.FileCount != want.fileCount || row.LastIndexedAt.UTC().Hour() != want.indexedHour {
					t.Errorf("row[%d] = %#v, want %#v", i, row, want)
				}
			}
		})
	}
}
