// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
)

// TestContentReaderSearchEntitiesByNameInRepositoriesBindsTheGrant pins the
// #5167 name search the code-relationships content fallback uses for a scoped
// caller: one statement, the grant in the same WHERE as the name match and
// ahead of the LIMIT, bound as one array parameter carrying every granted id.
func TestContentReaderSearchEntitiesByNameInRepositoriesBindsTheGrant(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
		rows: [][]driver.Value{{
			"entity-1", "repo-a", "src/app.go", "Function", "Handle",
			int64(1), int64(9), "go", "", []byte(`{}`),
		}},
	}})
	reader := NewContentReader(db)
	got, err := reader.SearchEntitiesByNameInRepositories(context.Background(), []string{"repo-a", "repo-b"}, "", "Handle", 2)
	if err != nil {
		t.Fatalf("SearchEntitiesByNameInRepositories() error = %v", err)
	}
	if len(got) != 1 || got[0].EntityID != "entity-1" {
		t.Fatalf("SearchEntitiesByNameInRepositories() = %#v, want entity-1", got)
	}
	if n := len(recorder.queries); n != 1 {
		t.Fatalf("issued %d statements, want exactly 1 for the whole grant", n)
	}
	query := strings.Join(strings.Fields(recorder.queries[0]), " ")
	where := strings.Index(query, "WHERE entity_name ILIKE '%' || $1 || '%' AND repo_id = ANY($2)")
	limit := strings.Index(query, "LIMIT $3")
	if where < 0 || limit < 0 || where > limit {
		t.Fatalf("query = %q, want the name match and the grant in one WHERE ahead of LIMIT $3", query)
	}
	bound := fmt.Sprint(recorder.args[0])
	for _, repoID := range []string{"repo-a", "repo-b"} {
		if !strings.Contains(bound, repoID) {
			t.Fatalf("bound args %s do not carry granted repository %q", bound, repoID)
		}
	}
}

// TestContentReaderSearchEntitiesByNameInRepositoriesEmptyGrantReadsNothing:
// an empty repository set is "no grant", never "no restriction".
func TestContentReaderSearchEntitiesByNameInRepositoriesEmptyGrantReadsNothing(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	reader := NewContentReader(db)
	got, err := reader.SearchEntitiesByNameInRepositories(context.Background(), nil, "", "Handle", 2)
	if err != nil || got != nil {
		t.Fatalf("SearchEntitiesByNameInRepositories(nil grant) = %#v, %v; want nil, nil", got, err)
	}
	if n := len(recorder.queries); n != 0 {
		t.Fatalf("empty grant issued %d statements, want 0", n)
	}
}
