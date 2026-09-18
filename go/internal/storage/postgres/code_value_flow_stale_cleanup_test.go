// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCodeValueFlowCurrentGenerationStoreListsActiveRepositoryScopes(t *testing.T) {
	t.Parallel()

	database := &codeValueFlowCurrentGenerationDB{
		rows: [][]any{
			{"scope-a", "gen-a"},
			{"scope-b", "gen-b"},
		},
	}
	store := NewCodeValueFlowCurrentGenerationStore(database)

	got, err := store.ListCurrentCodeValueFlowGenerations(context.Background(), "scope-a", 25)
	if err != nil {
		t.Fatalf("ListCurrentCodeValueFlowGenerations() error = %v, want nil", err)
	}
	want := []reducer.CodeValueFlowCurrentGeneration{
		{ScopeID: "scope-a", GenerationID: "gen-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b"},
	}
	if !equalCodeValueFlowGenerations(got, want) {
		t.Fatalf("generations = %+v, want %+v", got, want)
	}
	for _, wantSQL := range []string{
		"FROM ingestion_scopes AS scope",
		"JOIN scope_generations AS generation",
		"scope.active_generation_id IS NOT NULL",
		"scope.scope_kind = 'repository'",
		"scope.scope_id > $1",
		"ORDER BY scope.scope_id ASC",
		"LIMIT $2",
	} {
		if !strings.Contains(database.query, wantSQL) {
			t.Fatalf("candidate query missing %q:\n%s", wantSQL, database.query)
		}
	}
	if got := database.args[0]; got != "scope-a" {
		t.Fatalf("after scope arg = %v, want scope-a", got)
	}
	if got := database.args[1]; got != 25 {
		t.Fatalf("limit arg = %v, want 25", got)
	}
}

func TestCodeValueFlowCurrentGenerationStoreNoOpsWithoutPositiveLimit(t *testing.T) {
	t.Parallel()

	database := &codeValueFlowCurrentGenerationDB{
		rows: [][]any{{"scope-a", "gen-a"}},
	}
	store := NewCodeValueFlowCurrentGenerationStore(database)

	got, err := store.ListCurrentCodeValueFlowGenerations(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListCurrentCodeValueFlowGenerations() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(generations) = %d, want 0", len(got))
	}
	if database.query != "" {
		t.Fatalf("query = %q, want no query for non-positive limit", database.query)
	}
}

type codeValueFlowCurrentGenerationDB struct {
	rows  [][]any
	query string
	args  []any
}

func (database *codeValueFlowCurrentGenerationDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (database *codeValueFlowCurrentGenerationDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	database.query = query
	database.args = args
	return newProofRows(database.rows), nil
}

func equalCodeValueFlowGenerations(left, right []reducer.CodeValueFlowCurrentGeneration) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
