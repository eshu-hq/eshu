// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestInvestigateCodeTopicPathFirstPostgresLive exercises the generated #7033
// SQL against disposable PostgreSQL, including unscoped grant filtering and
// both sides of the path/content candidate partition. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN to an administrative postgres-database
// DSN and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestInvestigateCodeTopicPathFirstPostgresLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}

	_, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES
  ('repo-uncapped', 'src/needle-path.go', 'other', 'path', 1, 'go', now()),
  ('repo-uncapped', 'src/needle-both.go', 'needle', 'both', 1, 'go', now()),
  ('repo-uncapped', 'src/content-only.go', 'needle', 'content', 1, 'go', now()),
  ('repo-uncapped', 'src/no-match.go', 'other', 'none', 1, 'go', now()),
  ('repo-ungranted', 'src/needle-hidden.go', 'needle', 'hidden', 1, 'go', now())
`)
	if err != nil {
		t.Fatalf("seed uncapped partition: %v", err)
	}

	reader := NewContentReader(database)
	uncapped, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{"repo-uncapped"},
		Language:             "go",
		Terms:                []string{"needle"},
		Limit:                10,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() uncapped: %v", err)
	}
	wantPaths := []string{"src/content-only.go", "src/needle-both.go", "src/needle-path.go"}
	if len(uncapped) != len(wantPaths) {
		t.Fatalf("uncapped rows = %d, want %d", len(uncapped), len(wantPaths))
	}
	for index, row := range uncapped {
		if row.RepoID != "repo-uncapped" || row.RelativePath != wantPaths[index] || row.SourceKind != "file" {
			t.Fatalf("uncapped row %d = (%s, %s, %s), want repo-uncapped file %s",
				index, row.RepoID, row.RelativePath, row.SourceKind, wantPaths[index])
		}
		if row.PoolTruncated || row.Score != 1 || len(row.MatchedTerms) != 1 || row.MatchedTerms[0] != "needle" {
			t.Fatalf("uncapped row %d score/terms/cap = (%d, %v, %v), want (1, [needle], false)",
				index, row.Score, row.MatchedTerms, row.PoolTruncated)
		}
	}

	_, err = database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-dense', 'src/needle-' || lpad(i::text, 4, '0') || '.go',
       'other', 'dense', 1, 'go', now()
FROM generate_series(1, 4000) AS i
UNION ALL
SELECT 'repo-dense', 'src/only-content.go', 'needle', 'content', 1, 'go', now()
`)
	if err != nil {
		t.Fatalf("seed saturated path pool: %v", err)
	}
	// One term has a 4,000-row candidate cap. The complete path pool must
	// consume it before the content-only row can enter the scored candidates.
	dense, err := reader.InvestigateCodeTopic(ctx, CodeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{"repo-dense"},
		Language:             "go",
		Terms:                []string{"needle"},
		Limit:                5000,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() dense: %v", err)
	}
	if got, want := len(dense), 4000; got != want {
		t.Fatalf("dense rows = %d, want %d path candidates", got, want)
	}
	for index, row := range dense {
		if row.RepoID != "repo-dense" || !strings.Contains(row.RelativePath, "needle-") || !row.PoolTruncated {
			t.Fatalf("dense row %d = (%s, %s, capped=%v), want repo-dense path hit and capped marker",
				index, row.RepoID, row.RelativePath, row.PoolTruncated)
		}
	}
}
