// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

// TestContentReaderSearchEntitiesByExactNameThreadsTypeFilterPlaceholders closes
// the gap review found: every other exact-name shape test binds "" as the entity
// type, so the type-filtered combination -- where the type filter sits between
// repo_id and LIMIT and shifts the LIMIT placeholder -- had no coverage at all.
// A mis-numbered $N there binds the limit to a filter argument and silently
// returns the wrong rows, which no existing assertion could see (#6605 review).
//
// Both branches of contentEntityTypeFilter are covered, because they consume a
// DIFFERENT number of placeholders: a plain type takes one ($3, LIMIT $4), and
// an Elixir semantic type takes two ($3 and $4, LIMIT $5). The two-arg branch is
// the one that actually breaks arithmetic, so testing only a plain type would
// leave the riskier path unpinned.
func TestContentReaderSearchEntitiesByExactNameThreadsTypeFilterPlaceholders(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		entityType string
		wantSQL    []string
		wantArgs   []any
	}{
		{
			name:       "plain type consumes one placeholder",
			entityType: "Class",
			wantSQL:    []string{"entity_name = $1", "repo_id = $2", "entity_type = $3", "LIMIT $4"},
			wantArgs:   []any{"PaymentGateway", "repo://tenant-a/payments", "Class", int64(3)},
		},
		{
			name:       "semantic type consumes two placeholders",
			entityType: "guard",
			wantSQL: []string{
				"entity_name = $1", "repo_id = $2",
				"(entity_type = $3 AND coalesce(metadata ->> 'semantic_kind', '') = $4)",
				"LIMIT $5",
			},
			wantArgs: []any{"PaymentGateway", "repo://tenant-a/payments", "Function", "guard", int64(3)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
				{
					columns: []string{
						"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
						"start_line", "end_line", "language", "source_cache", "metadata",
					},
					rows: [][]driver.Value{},
				},
			})

			reader := NewContentReader(db)
			if _, err := reader.SearchEntitiesByExactName(
				context.Background(), "repo://tenant-a/payments", tc.entityType, "PaymentGateway", 3,
			); err != nil {
				t.Fatalf("SearchEntitiesByExactName() error = %v, want nil", err)
			}

			if len(recorder.queries) != 1 {
				t.Fatalf("len(recorder.queries) = %d, want 1", len(recorder.queries))
			}
			query := recorder.queries[0]
			for _, want := range tc.wantSQL {
				if !strings.Contains(query, want) {
					t.Fatalf("query %q is missing %q", query, want)
				}
			}
			if strings.Contains(query, "ILIKE") || strings.Contains(query, "LIKE") {
				t.Fatalf("the exact-name read is still a substring read: %q", query)
			}

			if got, want := len(recorder.args[0]), len(tc.wantArgs); got != want {
				t.Fatalf("len(query args) = %d, want %d", got, want)
			}
			for i, want := range tc.wantArgs {
				if wantInt, ok := want.(int64); ok {
					if got := numericDriverValue(t, recorder.args[0][i]); got != wantInt {
						t.Fatalf("query arg %d = %d, want %d", i, got, wantInt)
					}
					continue
				}
				if got := recorder.args[0][i]; got != want {
					t.Fatalf("query arg %d = %#v, want %#v", i, got, want)
				}
			}
		})
	}
}
