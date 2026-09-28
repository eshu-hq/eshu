// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/jackc/pgx/v5"
)

func TestSearchCodeCandidatesFiltersBeforeLimitAndUsesCustomPlan(t *testing.T) {
	t.Parallel()

	languageArg, err := array.Of([]string{"go"}).Value()
	if err != nil {
		t.Fatal(err)
	}
	columns := []string{
		"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
		"start_line", "end_line", "language", "source_cache", "metadata",
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: columns,
			rows:    [][]driver.Value{{"name-target", "repo-a", "c.go", "Function", "decode", int64(1), int64(2), "go", "", []byte(`{}`)}},
			queryContainsInOrder: []string{
				"WHERE repo_id = $1 AND entity_name ILIKE '%' || $2 || '%'",
				"AND language = ANY($3::text[])",
				"ORDER BY relative_path, start_line, entity_id", "LIMIT $4",
			},
			wantArgs: []driver.Value{pgx.QueryExecModeExec, "repo-a", "decode", languageArg, int64(2)},
		},
		{
			columns: columns,
			rows:    [][]driver.Value{{"source-target", "repo-a", "d.go", "Function", "other", int64(1), int64(2), "go", "decode", []byte(`{}`)}},
			queryContainsInOrder: []string{
				"WHERE repo_id = $1 AND source_cache ILIKE '%' || $2 || '%'",
				"AND language = ANY($3::text[])",
				"ORDER BY relative_path, start_line, entity_id", "LIMIT $4",
			},
			wantArgs: []driver.Value{pgx.QueryExecModeExec, "repo-a", "decode", languageArg, int64(2)},
		},
	})

	names, sources, err := NewContentReader(db).SearchCodeCandidates(context.Background(), "repo-a", "decode", "go", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].EntityID != "name-target" || len(sources) != 1 || sources[0].EntityID != "source-target" {
		t.Fatalf("names = %+v, sources = %+v; want both filtered candidates", names, sources)
	}
}

func TestSearchCodeCandidatesExactWithoutLanguage(t *testing.T) {
	t.Parallel()

	query, args := buildCodeCandidateQuery("repo-a", "decode", nil, 2, true, false)
	if strings.Contains(query, "language = ANY") || len(args) != 3 {
		t.Fatalf("exact query without language added a language filter: query=%s args=%#v", query, args)
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
		rows: [][]driver.Value{{"exact-target", "repo-a", "c.go", "Function", "decode", int64(1), int64(2), "go", "", []byte(`{}`)}},
		queryContainsInOrder: []string{
			"WHERE repo_id = $1 AND entity_name = $2",
			"ORDER BY relative_path, start_line, entity_id", "LIMIT $3",
		},
		wantArgs: []driver.Value{pgx.QueryExecModeExec, "repo-a", "decode", int64(2)},
	}})

	names, sources, err := NewContentReader(db).SearchCodeCandidates(context.Background(), "repo-a", "decode", "", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].EntityID != "exact-target" || len(sources) != 0 {
		t.Fatalf("names = %+v, sources = %+v; want exact name only", names, sources)
	}
}

func TestSearchCodeCandidatesExactWithLanguageVariants(t *testing.T) {
	t.Parallel()

	languageArg, err := array.Of([]string{"javascript", "jsx"}).Value()
	if err != nil {
		t.Fatal(err)
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
		rows: [][]driver.Value{{"exact-jsx", "repo-a", "c.jsx", "Function", "decode", int64(1), int64(2), "jsx", "", []byte(`{}`)}},
		queryContainsInOrder: []string{
			"WHERE repo_id = $1 AND entity_name = $2",
			"AND language = ANY($3::text[])",
			"ORDER BY relative_path, start_line, entity_id", "LIMIT $4",
		},
		wantArgs: []driver.Value{pgx.QueryExecModeExec, "repo-a", "decode", languageArg, int64(2)},
	}})

	names, sources, err := NewContentReader(db).SearchCodeCandidates(context.Background(), "repo-a", "decode", "javascript", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].EntityID != "exact-jsx" || len(sources) != 0 {
		t.Fatalf("names = %+v, sources = %+v; want one exact name and no source read", names, sources)
	}
}
