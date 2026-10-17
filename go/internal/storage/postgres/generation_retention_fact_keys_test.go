// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	factkeys "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
)

// factKeyTablesInOrder is the #7420 delete order every retention transaction
// must follow, written out here rather than read from the production list.
var factKeyTablesInOrder = []string{
	"relationship_reference_candidate_keys",
	"package_manifest_consumption_keys",
	"package_registry_identity_keys",
}

// factKeyTestRows answers each key sidecar delete statement with a distinct
// count so a test can tell the tables apart in RowsPruned.
var factKeyTestRows = map[string]int64{
	"relationship_reference_candidate_keys": 5,
	"package_manifest_consumption_keys":     2,
	"package_registry_identity_keys":        1,
}

func factKeyTestPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	}
}

// statementIndex returns the position of the first recorded statement that
// contains marker, or -1.
func statementIndex(statements []string, marker string) int {
	return slices.IndexFunc(statements, func(statement string) bool {
		return strings.Contains(statement, marker)
	})
}

// TestGenerationRetentionDeletesFactKeysBeforeGenerations proves the normal
// batch deletes every candidate's key sidecar rows explicitly (#7420), keyed
// by (scope_id, generation_id), table by table in the fixed lock order, after
// the changed-since ledger delete and before the scope_generations delete,
// and reports each table's deleted rows and a timed phase.
func TestGenerationRetentionDeletesFactKeysBeforeGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-a", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
			{"scope-b", "generation-b", "repository", now.Add(-10 * 24 * time.Hour), now.Add(-11 * 24 * time.Hour), false},
		},
		countRows: [][]any{
			{"generation-a", "fact_records", int64(3)},
			{"generation-b", "fact_records", int64(4)},
			{"generation-a", "package_manifest_consumption_keys", int64(4)},
		},
		execResults: generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2),
		factKeyRows: factKeyTestRows,
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), factKeyTestPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2", result.GenerationsPruned)
	}

	var want []string
	for _, table := range factKeyTablesInOrder {
		for _, pair := range [][2]string{{"scope-a", "generation-a"}, {"scope-b", "generation-b"}} {
			want = append(want, fmt.Sprintf("%s %s/%s", table, pair[0], pair[1]))
		}
	}
	var got []string
	for _, call := range database.factKeyExecs {
		table := ""
		for _, candidate := range factKeyTablesInOrder {
			if strings.Contains(call.query, "DELETE FROM "+candidate+"\n") {
				table = candidate
			}
		}
		if !strings.Contains(call.query, "WHERE scope_id = $1 AND generation_id = $2\n") || len(call.args) != 2 {
			t.Fatalf("fact key delete %q args %v, want the (scope_id, generation_id) statement", call.query, call.args)
		}
		got = append(got, fmt.Sprintf("%s %v/%v", table, call.args[0], call.args[1]))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fact key deletes =\n%v\nwant\n%v", got, want)
	}

	ledgerAt := statementIndex(database.statements, "del_activations")
	generationsAt := statementIndex(database.statements, "DELETE FROM scope_generations")
	firstKeyAt := statementIndex(database.statements, factkeys.DeleteMarker)
	lastKeyAt := len(database.statements) - 1 - statementIndex(reversed(database.statements), factkeys.DeleteMarker)
	if firstKeyAt < 0 || ledgerAt < 0 || generationsAt < 0 {
		t.Fatalf("statement positions ledger=%d keys=%d generations=%d, want all present", ledgerAt, firstKeyAt, generationsAt)
	}
	if firstKeyAt < ledgerAt || lastKeyAt > generationsAt {
		t.Fatalf("fact key deletes at [%d,%d], want after the ledger delete (%d) and before scope_generations (%d)",
			firstKeyAt, lastKeyAt, ledgerAt, generationsAt)
	}
	for table, perStatement := range factKeyTestRows {
		if got, want := result.RowsPruned[table], 2*perStatement; got != want {
			t.Errorf("RowsPruned[%s] = %d, want %d (two candidates)", table, got, want)
		}
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhaseDeleteFactKeys]; !ok {
		t.Fatalf("PhaseDurations has no %s phase", GenerationRetentionPhaseDeleteFactKeys)
	}
	// The pre-count names the key tables too: generation-a counted 4
	// manifest keys, generation-b none, and no other key table appears.
	wantCounted := map[string]int64{factKeyTablesInOrder[0]: 0, factKeyTablesInOrder[1]: 4, factKeyTablesInOrder[2]: 0}
	if !maps.Equal(result.FactKeyRowsCounted, wantCounted) {
		t.Errorf("FactKeyRowsCounted = %v, want %v", result.FactKeyRowsCounted, wantCounted)
	}
	wantPruned := map[string]int64{}
	for table, perStatement := range factKeyTestRows {
		wantPruned[table] = 2 * perStatement
	}
	if !maps.Equal(result.FactKeyRowsPruned, wantPruned) {
		t.Errorf("FactKeyRowsPruned = %v, want %v", result.FactKeyRowsPruned, wantPruned)
	}
}

// TestGenerationRetentionChunkDeletesPageFactKeysFirst proves the over-limit
// chunk path deletes the page's key sidecar rows (#7420) in the chunk's
// transaction, bound to the chunk's (scope_id, generation_id) and the exact
// page ids, before the page's fact delete, and merges the counts into
// RowsPruned after the normal batch.
func TestGenerationRetentionChunkDeletesPageFactKeysFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	page := make([][]any, 0, 100)
	pageIDs := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("huge-fact-%03d", i)
		pageIDs = append(pageIDs, id)
		page = append(page, []any{id, "content_entity", false, "", "", ""})
	}
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
			{"scope-a", "generation-small", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
		},
		prescreenRows: [][]any{{"generation-huge", int64(101)}, {"generation-small", int64(2)}},
		countRows:     [][]any{{"generation-small", "fact_records", int64(2)}},
		chunkPageRows: page,
		execResults: append(
			[]sql.Result{fakeRowsAffected{n: 100}},
			generationRetentionExecResults(1, 0, 0, 0, 0, 0, 0, 1)...,
		),
		factKeyRows: factKeyTestRows,
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), factKeyTestPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	var pageDeletes []fakeExecCall
	for _, call := range database.factKeyExecs {
		if strings.Contains(call.query, "fact_id = ANY($3::text[])") {
			pageDeletes = append(pageDeletes, call)
		}
	}
	if len(pageDeletes) != len(factKeyTablesInOrder) {
		t.Fatalf("chunk page key deletes = %d, want %d", len(pageDeletes), len(factKeyTablesInOrder))
	}
	for i, call := range pageDeletes {
		if !strings.Contains(call.query, "DELETE FROM "+factKeyTablesInOrder[i]+"\n") {
			t.Fatalf("chunk page key delete %d = %q, want %s", i, call.query, factKeyTablesInOrder[i])
		}
		ids, _ := call.args[2].([]string)
		if call.args[0] != "scope-a" || call.args[1] != "generation-huge" || !slices.Equal(ids, pageIDs) {
			t.Fatalf("chunk page key delete %d args = %v, %v, %d ids; want scope-a, generation-huge and the page", i, call.args[0], call.args[1], len(ids))
		}
	}
	pageKeyAt := statementIndex(database.statements, factkeys.DeleteMarker+" (chunk page)")
	factDeleteAt := statementIndex(database.statements, "retention: over-limit chunk fact delete")
	if pageKeyAt < 0 || factDeleteAt < 0 || pageKeyAt > factDeleteAt {
		t.Fatalf("chunk page key delete at %d, fact delete at %d: want key deletes first", pageKeyAt, factDeleteAt)
	}
	// generation-small's whole-generation delete plus the chunk page's.
	for table, perStatement := range factKeyTestRows {
		if got, want := result.RowsPruned[table], 2*perStatement; got != want {
			t.Errorf("RowsPruned[%s] = %d, want %d (batch plus chunk page)", table, got, want)
		}
	}
	// FactKeyRowsPruned is the whole-generation delete alone, the figure its
	// pre-count describes; the chunk page's keys stay out of it.
	if !maps.Equal(result.FactKeyRowsPruned, factKeyTestRows) {
		t.Errorf("FactKeyRowsPruned = %v, want the batch delete alone %v", result.FactKeyRowsPruned, factKeyTestRows)
	}
}

func reversed(values []string) []string {
	out := slices.Clone(values)
	slices.Reverse(out)
	return out
}
