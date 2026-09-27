// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSearchEntitiesSingleRepoAvoidsNamedPreparedStatementLive checks the real
// pgx database/sql adapter against a disposable PostgreSQL database. A named
// prepared statement for this query can eventually receive a generic plan,
// which scans the global trigram index for a one-repository page.
func TestSearchEntitiesSingleRepoAvoidsNamedPreparedStatementLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	db.SetMaxOpenConns(1)

	_, err := db.ExecContext(ctx, `
		CREATE TABLE content_entities (
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
		)
	`)
	if err != nil {
		t.Fatalf("create content_entities fixture: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO content_entities
			(entity_id, repo_id, relative_path, entity_type, entity_name,
			 start_line, end_line, language, source_cache, metadata)
		VALUES
			('a', 'repo-1', 'a.go', 'Function', 'first', 1, 2, 'go', 'alpha', '{}'),
			('b', 'repo-1', 'b.go', 'Function', 'second', 2, 3, 'go', 'beta', '{}'),
			('c', 'repo-1', 'c.go', 'Function', 'third', 3, 4, 'go', 'gamma', '{}'),
			('x', 'repo-2', 'x.go', 'Function', 'other', 1, 2, 'go', 'alpha', '{}')
	`)
	if err != nil {
		t.Fatalf("seed content_entities fixture: %v", err)
	}

	reader := NewContentReader(db)
	for i := 0; i < 7; i++ {
		rows, err := reader.SearchEntities(ctx, "repo-1", nil, "a", 2, 1)
		if err != nil {
			t.Fatalf("SearchEntities() call %d: %v", i+1, err)
		}
		if len(rows) != 2 || rows[0].EntityID != "b" || rows[1].EntityID != "c" {
			t.Fatalf("SearchEntities() call %d = %+v, want ordered page [b c]", i+1, rows)
		}
	}

	var prepared int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_prepared_statements
		WHERE statement LIKE '%' || 'FROM content_' || 'entities%'
	`).Scan(&prepared)
	if err != nil {
		t.Fatalf("inspect pg_prepared_statements: %v", err)
	}
	if prepared != 0 {
		t.Fatalf("scoped page left %d named prepared statements; want 0", prepared)
	}

	// A normally cached read on this same connection must appear in the view;
	// otherwise the zero above could be an introspection false green.
	var total int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM content_entities").Scan(&total); err != nil {
		t.Fatalf("run cached control query: %v", err)
	}
	if total != 4 {
		t.Fatalf("cached control count = %d, want 4", total)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_prepared_statements
		WHERE statement LIKE '%' || 'FROM content_' || 'entities%'
	`).Scan(&prepared); err != nil {
		t.Fatalf("inspect cached control statement: %v", err)
	}
	if prepared != 1 {
		t.Fatalf("cached control left %d named prepared statements; want 1", prepared)
	}
}
