// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9304 disposable PostgreSQL proof that the PHP configuration reader walks all 120 exact-name rows across repository and page boundaries, honors repository grants, and withholds source_cache on a 120000-row fixture with production-shaped indexes
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestPHPConfigKeysetLive proves the shipped exact-name page reads every
// synthetic match once, across repository and page boundaries.
func TestPHPConfigKeysetLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		3*time.Minute,
	)
	statements := []string{
		`CREATE EXTENSION pg_trgm`,
		`CREATE TABLE content_entities (
            entity_id text PRIMARY KEY,
            repo_id text NOT NULL,
            relative_path text NOT NULL,
            entity_type text NOT NULL,
            entity_name text NOT NULL,
            start_line integer NOT NULL,
            end_line integer NOT NULL,
            language text,
            source_cache text,
            metadata jsonb NOT NULL
        )`,
		`INSERT INTO content_entities
            (entity_id, repo_id, relative_path, entity_type, entity_name,
             start_line, end_line, language, source_cache, metadata)
            SELECT 'entity-' || lpad(g::text, 7, '0'),
                   'repo-' || lpad((((g - 1) / 1000) % 60 + 1)::text, 3, '0'),
                   'src/file-' || lpad(g::text, 7, '0') || '.ini',
                   'Variable',
                   CASE WHEN g % 1000 = 0 THEN 'php_server_mode:mod_php'
                        WHEN g % 10 = 0 THEN 'php_directive:memory_limit'
                        ELSE 'other:' || g::text END,
                   1, 1, 'php', 'raw fixture source must not escape',
                   '{"value":"256M"}'::jsonb
            FROM generate_series(1, 120000) AS g`,
		`CREATE INDEX content_entities_name_trgm_idx
            ON content_entities USING gin (entity_name gin_trgm_ops)`,
		`CREATE INDEX content_entities_repo_path_start_idx
            ON content_entities (repo_id, relative_path, start_line, entity_id)`,
		`VACUUM ANALYZE content_entities`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed PHP configuration fixture: %v\n%s", err, statement)
		}
	}
	reader := NewContentReader(db)
	var cursor *querycontract.PHPConfigCursor
	seen := make(map[string]bool)
	for {
		rows, err := reader.SearchPHPConfigEntities(ctx, nil, "php_server_mode:mod_php", cursor, 7)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if seen[row.EntityID] {
				t.Fatalf("duplicate entity %s", row.EntityID)
			}
			if row.EntityName != "php_server_mode:mod_php" || row.SourceCache != "" {
				t.Fatalf("wrong or unsafe row: %+v", row)
			}
			seen[row.EntityID] = true
		}
		if len(rows) < 7 {
			break
		}
		last := rows[len(rows)-1]
		cursor = &querycontract.PHPConfigCursor{RepoID: last.RepoID, RelativePath: last.RelativePath, StartLine: last.StartLine, EntityID: last.EntityID}
	}
	if len(seen) != 120 {
		t.Fatalf("seen %d mode rows, want 120", len(seen))
	}
	scoped, err := reader.SearchPHPConfigEntities(ctx, []string{"repo-003"}, "php_server_mode:mod_php", nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 2 || scoped[0].RepoID != "repo-003" || scoped[1].RepoID != "repo-003" {
		t.Fatalf("scoped rows = %+v", scoped)
	}
	denied, err := reader.SearchPHPConfigEntities(ctx, []string{}, "php_server_mode:mod_php", nil, 7)
	if err != nil || len(denied) != 0 {
		t.Fatalf("empty grant returned %d rows, error %v", len(denied), err)
	}
}
