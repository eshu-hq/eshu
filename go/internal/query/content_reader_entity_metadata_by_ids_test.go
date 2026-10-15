// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

var entityMetadataByIDColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "language", "metadata",
}

// entityMetadataByIDResult is the fake-driver answer for the #7247
// id-anchored enrichment read: its nine projected columns (no source_cache)
// and the keyed statement shape. The language-query tests that drive a real
// ContentReader for a content-id-anchored label queue this instead of the
// filtered page read they queued before #7247.
func entityMetadataByIDResult(rows ...[]driver.Value) contentReaderQueryResult {
	return contentReaderQueryResult{
		columns:       entityMetadataByIDColumns,
		queryContains: []string{"entity_id = ANY($1)"},
		rows:          rows,
	}
}

// TestEntityMetadataByIDsStatementShape pins the #7247 id-anchored read: one
// statement keyed by entity_id = ANY, the caller's repository scope bound in
// the same WHERE, and no source_cache projection (the enrichment merges
// metadata only, and source_cache is the widest column on the table).
func TestEntityMetadataByIDsStatementShape(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		lookup     taxonomy.LanguageEntityIDLookup
		wantFilter string
		wantArgs   []any
	}{
		{
			name:       "grant",
			lookup:     taxonomy.LanguageEntityIDLookup{EntityIDs: []string{"e1", "e2"}, AllowedRepositoryIDs: []string{"r1", "r2"}},
			wantFilter: "WHERE entity_id = ANY($1) AND repo_id = ANY($2)",
		},
		{
			name:       "repository",
			lookup:     taxonomy.LanguageEntityIDLookup{EntityIDs: []string{"e1"}, RepoID: "r1", AllowedRepositoryIDs: []string{"r1", "r2"}},
			wantFilter: "WHERE entity_id = ANY($1) AND repo_id = $2",
		},
		{
			name:       "unscoped",
			lookup:     taxonomy.LanguageEntityIDLookup{EntityIDs: []string{"e1"}},
			wantFilter: "WHERE entity_id = ANY($1)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			statement, args := buildEntityMetadataByIDsQuery(uniqueStrings(tc.lookup.EntityIDs), tc.lookup)
			if !strings.Contains(statement, tc.wantFilter) {
				t.Fatalf("statement missing %q:\n%s", tc.wantFilter, statement)
			}
			for _, banned := range []string{"source_cache", "ORDER BY", "ILIKE", "EXISTS"} {
				if strings.Contains(statement, banned) {
					t.Fatalf("statement carries %q; the id lookup must stay a bare keyed read:\n%s", banned, statement)
				}
			}
			if got, want := len(args), strings.Count(statement, "$"); got != want {
				t.Fatalf("args = %d, placeholders = %d", got, want)
			}
		})
	}
}

// TestEntityMetadataByIDsBindsArgsAndScansWithoutSourceCache drives the read
// through the content reader and the fake driver: the exact bind values, and
// a nine-column row scanned into EntityContent with metadata decoded.
func TestEntityMetadataByIDsBindsArgsAndScansWithoutSourceCache(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:              entityMetadataByIDColumns,
		queryContainsInOrder: []string{"FROM content_entities", "entity_id = ANY($1)", "repo_id = ANY($2)"},
		rows: [][]driver.Value{{
			"e1", "r1", "src/a.php", "Function", "a", int64(3), int64(9), "php", []byte(`{"docstring":"doc"}`),
		}},
		wantArgs: []driver.Value{`{"e1","e2"}`, `{"r1"}`},
	}})
	reader := NewContentReader(db)

	got, err := reader.ListEntityMetadataByIDsForAccess(t.Context(), taxonomy.LanguageEntityIDLookup{
		EntityIDs:            []string{"e1", "e2", "e1", ""},
		AllowedRepositoryIDs: []string{"r1"},
	})
	if err != nil {
		t.Fatalf("ListEntityMetadataByIDsForAccess() error = %v", err)
	}
	if len(got) != 1 || got[0].EntityID != "e1" || got[0].RepoID != "r1" || got[0].StartLine != 3 {
		t.Fatalf("rows = %+v, want the one e1 row", got)
	}
	if got[0].Metadata["docstring"] != "doc" {
		t.Fatalf("metadata = %#v, want decoded docstring", got[0].Metadata)
	}
	if got[0].SourceCache != "" {
		t.Fatalf("source_cache = %q, want empty: the lookup does not read it", got[0].SourceCache)
	}
}

// TestEntityMetadataByIDsSkipsTheStatementForNoIDs pins that an empty id set
// issues no statement (the fake driver fails an unexpected query).
func TestEntityMetadataByIDsSkipsTheStatementForNoIDs(t *testing.T) {
	t.Parallel()

	reader := NewContentReader(openContentReaderTestDB(t, nil))
	got, err := reader.ListEntityMetadataByIDsForAccess(t.Context(), taxonomy.LanguageEntityIDLookup{EntityIDs: []string{"", ""}})
	if err != nil || len(got) != 0 {
		t.Fatalf("ListEntityMetadataByIDsForAccess(no ids) = %v, %v; want nil, nil", got, err)
	}
}

// TestEntityMetadataByIDsRejectsAnOversizedIDSet pins the bound: more ids than
// one language-query page is a caller bug, refused before any statement.
func TestEntityMetadataByIDsRejectsAnOversizedIDSet(t *testing.T) {
	t.Parallel()

	ids := make([]string, 0, taxonomy.MaxLanguageEntityIDLookup+1)
	for i := range taxonomy.MaxLanguageEntityIDLookup + 1 {
		ids = append(ids, fmt.Sprintf("e%d", i))
	}
	reader := NewContentReader(openContentReaderTestDB(t, nil))
	if _, err := reader.ListEntityMetadataByIDsForAccess(t.Context(), taxonomy.LanguageEntityIDLookup{EntityIDs: ids}); err == nil {
		t.Fatalf("ListEntityMetadataByIDsForAccess(%d ids) error = nil, want a bound error", len(ids))
	}
}
