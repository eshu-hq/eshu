// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// pagingQueryer is a test double that records every query and its args so the
// loader's keyset pagination (LIMIT + advancing cursor) can be asserted. It
// returns canned typed rows keyed by a substring of the query.
type pagingQueryer struct {
	calls    []pagingCall
	repoID   string
	entities []pagingEntityRow
	files    []pagingFileRow
}

type pagingCall struct {
	query string
	args  []any
}

type pagingEntityRow struct {
	entityID     string
	repoID       string
	relativePath string
	entityType   string
	entityName   string
	startLine    int64
	endLine      int64
	language     string
	artifactType string
	sourceCache  string
	metadata     []byte
	indexedAt    time.Time
}

type pagingFileRow struct {
	repoID       string
	relativePath string
	language     string
	artifactType string
	content      string
	indexedAt    time.Time
}

func (q *pagingQueryer) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	q.calls = append(q.calls, pagingCall{query: query, args: args})

	switch {
	case strings.Contains(query, "FROM ingestion_scopes"):
		return &pagingScalarRows{value: q.repoID}, nil
	case strings.Contains(query, "FROM content_entities"):
		return q.entityPage(args), nil
	case strings.Contains(query, "FROM content_files"):
		return q.filePage(args), nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

// entityPage returns the entity rows after the cursor (last arg before limit),
// honoring the page-size LIMIT so the loader must advance to drain all rows.
func (q *pagingQueryer) entityPage(args []any) Rows {
	cursor, limit := keysetArgs(args)
	rows := make([][]any, 0, limit)
	for _, r := range q.entities {
		if r.entityID <= cursor {
			continue
		}
		rows = append(rows, []any{
			r.entityID, r.repoID, r.relativePath, r.entityType, r.entityName,
			r.startLine, r.endLine, r.language, r.artifactType, r.sourceCache,
			r.metadata, r.indexedAt,
		})
		if len(rows) >= limit {
			break
		}
	}
	return &pagingTypedRows{rows: rows}
}

func (q *pagingQueryer) filePage(args []any) Rows {
	cursor, limit := keysetArgs(args)
	rows := make([][]any, 0, limit)
	for _, r := range q.files {
		if r.relativePath <= cursor {
			continue
		}
		rows = append(rows, []any{
			r.repoID, r.relativePath, r.language, r.artifactType, r.content, r.indexedAt,
		})
		if len(rows) >= limit {
			break
		}
	}
	return &pagingTypedRows{rows: rows}
}

// keysetArgs extracts the cursor and limit from a keyset query's args. Layout:
// $1=repoID, $2=cursor, $3=limit.
func keysetArgs(args []any) (string, int) {
	cursor := ""
	limit := 0
	if len(args) >= 2 {
		if s, ok := args[1].(string); ok {
			cursor = s
		}
	}
	if len(args) >= 3 {
		switch v := args[2].(type) {
		case int:
			limit = v
		case int64:
			limit = int(v)
		}
	}
	if limit <= 0 {
		limit = 1 << 30
	}
	return cursor, limit
}

type pagingScalarRows struct {
	value string
	done  bool
}

func (r *pagingScalarRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *pagingScalarRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scalar scan dest = %d, want 1", len(dest))
	}
	switch target := dest[0].(type) {
	case *string:
		*target = r.value
	case *sql.NullString:
		*target = sql.NullString{String: r.value, Valid: r.value != ""}
	default:
		return fmt.Errorf("unsupported scalar scan target %T", dest[0])
	}
	return nil
}

func (r *pagingScalarRows) Err() error   { return nil }
func (r *pagingScalarRows) Close() error { return nil }

type pagingTypedRows struct {
	rows  [][]any
	index int
}

func (r *pagingTypedRows) Next() bool { return r.index < len(r.rows) }

func (r *pagingTypedRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan dest = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			target2, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] = %T, want string", i, row[i])
			}
			*target = target2
		case *int:
			target2, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] = %T, want int64", i, row[i])
			}
			*target = int(target2)
		case *[]byte:
			switch v := row[i].(type) {
			case nil:
				*target = nil
			case []byte:
				*target = v
			default:
				return fmt.Errorf("row[%d] = %T, want []byte", i, row[i])
			}
		case *time.Time:
			target2, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] = %T, want time.Time", i, row[i])
			}
			*target = target2
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.index++
	return nil
}

func (r *pagingTypedRows) Err() error   { return nil }
func (r *pagingTypedRows) Close() error { return nil }

// TestStreamSearchDocumentSourcesPaginatesEntitiesWithLimit proves the loader
// issues bounded keyset queries (LIMIT present, cursor advancing) and yields
// every entity row across pages with no duplicates or gaps. This is the #3440
// regression: the old loader issued one unbounded SELECT with no LIMIT.
func TestStreamSearchDocumentSourcesPaginatesEntitiesWithLimit(t *testing.T) {
	t.Parallel()

	const total = 5
	entities := make([]pagingEntityRow, total)
	for i := range entities {
		entities[i] = pagingEntityRow{
			entityID:    fmt.Sprintf("e-%02d", i),
			repoID:      "repo-1",
			entityType:  "Function",
			entityName:  fmt.Sprintf("Fn%d", i),
			sourceCache: "func(){}",
			metadata:    []byte(`{}`),
			indexedAt:   time.Unix(0, 0).UTC(),
		}
	}
	q := &pagingQueryer{repoID: "repo-1", entities: entities}
	// Small page size forces multiple keyset pages over the 5-row fixture.
	loader := EshuSearchDocumentSourceLoader{db: q, entityPageSize: 2}

	var got []string
	err := loader.StreamSearchDocumentSources(context.Background(), "scope-1", "gen-1",
		func(page reducer.SearchDocumentProjectionInput) error {
			for _, e := range page.ContentEntities {
				got = append(got, e.EntityID)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("StreamSearchDocumentSources error = %v", err)
	}

	if len(got) != total {
		t.Fatalf("yielded %d entities, want %d (no gaps/dupes): %v", len(got), total, got)
	}
	for i, id := range got {
		want := fmt.Sprintf("e-%02d", i)
		if id != want {
			t.Fatalf("entity[%d] = %q, want %q (ordered, no gaps)", i, id, want)
		}
	}

	// Every content_entities query must carry a LIMIT and advance the cursor.
	var entityCalls []pagingCall
	for _, c := range q.calls {
		if strings.Contains(c.query, "FROM content_entities") {
			entityCalls = append(entityCalls, c)
		}
	}
	if len(entityCalls) < 2 {
		t.Fatalf("entity query count = %d, want >= 2 (paginated)", len(entityCalls))
	}
	for i, c := range entityCalls {
		if !strings.Contains(c.query, "LIMIT") {
			t.Fatalf("entity query %d missing LIMIT: %q", i, c.query)
		}
		if !strings.Contains(c.query, "entity_id >") {
			t.Fatalf("entity query %d missing keyset predicate: %q", i, c.query)
		}
	}
	// Cursor must strictly advance between the first two pages.
	first, _ := entityCalls[0].args[1].(string)
	second, _ := entityCalls[1].args[1].(string)
	if first >= second {
		t.Fatalf("cursor did not advance: first=%q second=%q", first, second)
	}
}

// TestStreamSearchDocumentSourcesPaginatesFilesWithLimit proves files are
// keyset-paginated by relative_path with bounded LIMIT queries and yield every
// row with no gaps.
func TestStreamSearchDocumentSourcesPaginatesFilesWithLimit(t *testing.T) {
	t.Parallel()

	const total = 4
	files := make([]pagingFileRow, total)
	for i := range files {
		files[i] = pagingFileRow{
			repoID:       "repo-1",
			relativePath: fmt.Sprintf("dir/file_%02d.go", i),
			content:      "package main",
			indexedAt:    time.Unix(0, 0).UTC(),
		}
	}
	q := &pagingQueryer{repoID: "repo-1", files: files}
	// Small page size forces multiple keyset pages over the 4-row fixture.
	loader := EshuSearchDocumentSourceLoader{db: q, filePageSize: 2}

	var got []string
	err := loader.StreamSearchDocumentSources(context.Background(), "scope-1", "gen-1",
		func(page reducer.SearchDocumentProjectionInput) error {
			for _, f := range page.ContentFiles {
				got = append(got, f.RelativePath)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("StreamSearchDocumentSources error = %v", err)
	}
	if len(got) != total {
		t.Fatalf("yielded %d files, want %d: %v", len(got), total, got)
	}

	var fileCalls []pagingCall
	for _, c := range q.calls {
		if strings.Contains(c.query, "FROM content_files") {
			fileCalls = append(fileCalls, c)
		}
	}
	if len(fileCalls) < 2 {
		t.Fatalf("file query count = %d, want >= 2 (paginated)", len(fileCalls))
	}
	for i, c := range fileCalls {
		if !strings.Contains(c.query, "LIMIT") {
			t.Fatalf("file query %d missing LIMIT: %q", i, c.query)
		}
		if !strings.Contains(c.query, "relative_path >") {
			t.Fatalf("file query %d missing keyset predicate: %q", i, c.query)
		}
	}
}
