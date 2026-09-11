// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSearchEntitiesByLanguageAndTypeGatesPageReadOnSameFilters is the #6540
// regression: the paged statement must carry an existence gate over the same
// filters, so a combination matching nothing short-circuits before the
// ORDER BY ... LIMIT walk. The uncorrelated EXISTS pulls up into an initplan
// behind a one-time filter; the live plan-shape test proves the
// short-circuit, while this pins the shipped shape it depends on.
func TestSearchEntitiesByLanguageAndTypeGatesPageReadOnSameFilters(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			queryContainsInOrder: []string{
				"EXISTS (SELECT 1 FROM content_entities WHERE",
				"language",
				"ORDER BY relative_path",
			},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.SearchEntitiesByLanguageAndTypeForAccess(t.Context(), querycontract.LanguageEntitySearch{
		Language:   "hcl",
		EntityType: "Function",
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() returned %d entities, want 0", len(got))
	}
}

// TestSearchEntitiesByLanguageAndTypeGateKeepsBindArgs pins that the gate
// reuses the filter placeholders: the shipped statement takes exactly the
// builder's args in the builder's order, with nothing appended for the
// subquery.
func TestSearchEntitiesByLanguageAndTypeGateKeepsBindArgs(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{{
				"e1", "r1", "a.go", "Function", "n", int64(1), int64(2), "go", "", []byte("{}"),
			}},
			wantArgs: []driver.Value{"Function", "go", int64(50)},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.SearchEntitiesByLanguageAndTypeForAccess(t.Context(), querycontract.LanguageEntitySearch{
		Language:   "go",
		EntityType: "Function",
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0].EntityID != "e1" {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess() = %+v, want the one paged row", got)
	}
}
