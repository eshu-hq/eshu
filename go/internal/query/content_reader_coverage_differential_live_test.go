// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// legacyEntityCoverage is what ContentReader.RepositoryCoverage derived from
// content_entities before #7126, via three separate whole-repository scans
// (count, max(indexed_at), GROUP BY entity_type). It is the differential
// baseline for the single-pass read and is frozen here because production no
// longer contains those statements.
func legacyEntityCoverage(t *testing.T, ctx context.Context, db *sql.DB, repoID string) (
	int, time.Time, []querycontract.RepositoryEntityTypeCount,
) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_entities WHERE repo_id = $1`, repoID).Scan(&count); err != nil {
		t.Fatalf("legacy entity count: %v", err)
	}
	var maxIndexed sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT max(indexed_at) as indexed_at
		FROM content_entities
		WHERE repo_id = $1
	`, repoID).Scan(&maxIndexed); err != nil {
		t.Fatalf("legacy entity max indexed_at: %v", err)
	}
	var indexedAt time.Time
	if maxIndexed.Valid {
		indexedAt = maxIndexed.Time.UTC()
	}
	rows, err := db.QueryContext(ctx, `
		SELECT entity_type, count(*) as entity_count
		FROM content_entities
		WHERE repo_id = $1
		GROUP BY entity_type
		ORDER BY entity_count DESC, entity_type
	`, repoID)
	if err != nil {
		t.Fatalf("legacy entity type distribution: %v", err)
	}
	defer func() { _ = rows.Close() }()
	types := make([]querycontract.RepositoryEntityTypeCount, 0)
	for rows.Next() {
		var entityType querycontract.RepositoryEntityTypeCount
		if err := rows.Scan(&entityType.EntityType, &entityType.Count); err != nil {
			t.Fatalf("scan legacy entity type: %v", err)
		}
		types = append(types, entityType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("legacy entity type rows: %v", err)
	}
	return count, indexedAt, types
}

// seedCoverageEntities inserts n content_entities rows for repoID cycling
// through the entity types, with indexed_at spread over a day so the maximum is
// a specific row, not a constant.
func seedCoverageEntities(t *testing.T, ctx context.Context, db *sql.DB, repoID string, n int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line,
                              source_cache, indexed_at)
SELECT $1 || ':' || i, $1, 'src/f' || (i % 5000) || '.go',
       (ARRAY['Function','Function','Function','Class','Variable','Module','TerraformResource'])[1 + (i % 7)],
       'e' || i, 1, 2, repeat('x', 200),
       timestamptz '2026-04-01 00:00:00+00' + ((i * 7919) % 86400) * interval '1 second'
FROM generate_series(1, $2::int) AS i
`, repoID, n); err != nil {
		t.Fatalf("seed content_entities for %s: %v", repoID, err)
	}
}

// TestRepositoryCoverageSinglePassMatchesLegacyLive proves the #7126 single
// grouped statement returns the entity total, newest indexed_at, and ordered
// type distribution the three legacy statements returned, for a populated
// repository, a repository whose types tie on count, a repository with no
// entities, and an unknown repository, on real PostgreSQL.
func TestRepositoryCoverageSinglePassMatchesLegacyLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedCoverageEntities(t, ctx, db, "repo:big", 7003)
	seedCoverageEntities(t, ctx, db, "repo:tie", 14) // two of each type: ties on count
	seedCoverageEntities(t, ctx, db, "repo:one", 1)
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at, language)
VALUES ('repo:files-only', 'a.go', 'x', 'h', 1, now(), 'go')
`); err != nil {
		t.Fatalf("seed files-only repo: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	reader := NewContentReader(db)
	for _, repoID := range []string{"repo:big", "repo:tie", "repo:one", "repo:files-only", "repo:absent"} {
		t.Run(repoID, func(t *testing.T) {
			wantCount, wantIndexed, wantTypes := legacyEntityCoverage(t, ctx, db, repoID)
			got, err := reader.RepositoryCoverage(ctx, repoID)
			if err != nil {
				t.Fatalf("RepositoryCoverage: %v", err)
			}
			if got.EntityCount != wantCount {
				t.Fatalf("EntityCount = %d, want %d", got.EntityCount, wantCount)
			}
			if !got.EntityIndexedAt.Equal(wantIndexed) || got.EntityIndexedAt.IsZero() != wantIndexed.IsZero() {
				t.Fatalf("EntityIndexedAt = %v, want %v", got.EntityIndexedAt, wantIndexed)
			}
			if !reflect.DeepEqual(got.EntityTypes, wantTypes) {
				t.Fatalf("EntityTypes = %#v, want %#v", got.EntityTypes, wantTypes)
			}
			if got.EntityTypes == nil {
				t.Fatal("EntityTypes = nil, want a non-nil (possibly empty) slice")
			}
		})
	}
}

// TestRepositoryCoverageScaleProofLive measures the three legacy scans against
// the single production statement on ESHU_TEST_CONTENT_COVERAGE_ROWS entities in
// one repository, interleaved with alternating first mover, and logs medians.
// Skipped unless the variable is set.
func TestRepositoryCoverageScaleProofLive(t *testing.T) {
	rows, _ := strconv.Atoi(os.Getenv("ESHU_TEST_CONTENT_COVERAGE_ROWS"))
	if rows <= 0 {
		t.Skip("set ESHU_TEST_CONTENT_COVERAGE_ROWS to run the scaled measurement")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedCoverageEntities(t, ctx, db, "repo:big", rows)
	seedCoverageEntities(t, ctx, db, "repo:neighbor", rows/4) // other repos share the table and index
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	wantCount, _, wantTypes := legacyEntityCoverage(t, ctx, db, "repo:big")
	got, err := NewContentReader(db).RepositoryCoverage(ctx, "repo:big")
	if err != nil || got.EntityCount != wantCount || !reflect.DeepEqual(got.EntityTypes, wantTypes) {
		t.Fatalf("scale differential mismatch: err=%v count=%d/%d", err, got.EntityCount, wantCount)
	}

	legacyStatements := []string{
		`SELECT count(*) FROM content_entities WHERE repo_id = $1`,
		`SELECT max(indexed_at) as indexed_at FROM content_entities WHERE repo_id = $1`,
		`SELECT entity_type, count(*) as entity_count FROM content_entities WHERE repo_id = $1 GROUP BY entity_type ORDER BY entity_count DESC, entity_type`,
	}
	timeLegacy := func() float64 {
		var total float64
		for _, statement := range legacyStatements {
			total += explainCoverageStatement(t, ctx, db, statement)
		}
		return total
	}
	timeNew := func() float64 { return explainCoverageStatement(t, ctx, db, repositoryEntityCoverageSQL) }

	var legacyMS, newMS []float64
	for i := 0; i < 9; i++ {
		if i%2 == 0 {
			legacyMS = append(legacyMS, timeLegacy())
			newMS = append(newMS, timeNew())
		} else {
			newMS = append(newMS, timeNew())
			legacyMS = append(legacyMS, timeLegacy())
		}
	}
	t.Logf("COVERAGE_SCALE rows=%d legacy_three_scans_median_ms=%.2f legacy_first_ms=%.2f single_pass_median_ms=%.2f single_pass_first_ms=%.2f",
		rows, median(legacyMS), legacyMS[0], median(newMS), newMS[1])
}

// explainCoverageStatement returns the EXPLAIN ANALYZE execution time in
// milliseconds of one coverage statement bound to repo:big.
func explainCoverageStatement(t *testing.T, ctx context.Context, db *sql.DB, statement string) float64 {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, "repo:big").Scan(&raw); err != nil {
		t.Fatalf("explain coverage statement: %v", err)
	}
	return executionTimeMS(t, raw)
}
