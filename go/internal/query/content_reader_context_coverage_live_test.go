// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestRepositoryContextCoverageMatchesFullCoverageLive proves the context
// summary's file count and ordered language groups match the existing full
// coverage read on a populated and an empty repository in real PostgreSQL.
func TestRepositoryContextCoverageMatchesFullCoverageLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		8*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at, language)
SELECT 'repo:big', 'src/f' || i || '.go', 'x', 'h' || i, 1, now(),
       CASE WHEN i % 13 = 0 THEN NULL WHEN i % 3 = 0 THEN 'yaml' ELSE 'go' END
FROM generate_series(1, 12403) AS i
`); err != nil {
		t.Fatalf("seed files: %v", err)
	}
	entityRows := 24172
	if raw := os.Getenv("ESHU_TEST_CONTEXT_COVERAGE_ENTITY_ROWS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid ESHU_TEST_CONTEXT_COVERAGE_ENTITY_ROWS %q", raw)
		}
		entityRows = parsed
	}
	seedCoverageEntities(t, ctx, db, "repo:big", entityRows)
	if _, err := db.ExecContext(ctx, "ANALYZE content_files"); err != nil {
		t.Fatalf("analyze files: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("analyze entities: %v", err)
	}

	reader := NewContentReader(db)
	for _, repoID := range []string{"repo:big", "repo:empty"} {
		t.Run(repoID, func(t *testing.T) {
			fullStarted := time.Now()
			full, err := reader.RepositoryCoverage(ctx, repoID)
			fullDuration := time.Since(fullStarted)
			if err != nil {
				t.Fatalf("full coverage: %v", err)
			}
			contextStarted := time.Now()
			narrow, err := reader.RepositoryContextCoverage(ctx, repoID)
			contextDuration := time.Since(contextStarted)
			if err != nil {
				t.Fatalf("context coverage: %v", err)
			}
			if full.Available != narrow.Available || full.FileCount != narrow.FileCount ||
				!reflect.DeepEqual(full.Languages, narrow.Languages) {
				t.Fatalf("context coverage = %#v, full coverage = %#v", narrow, full)
			}
			if repoID == "repo:big" && full.EntityCount == 0 {
				t.Fatal("fixture lacks entities needed to exercise the avoided aggregate")
			}
			t.Logf("repo=%s full=%s context=%s files=%d languages=%d",
				repoID, fullDuration, contextDuration, narrow.FileCount, len(narrow.Languages))
		})
	}
}
