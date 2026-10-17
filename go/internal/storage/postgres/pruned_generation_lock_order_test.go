// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionLocksWorkRowsInWorkItemOrderBeforeDelete pins the
// #8261 lock step of a retention pass: the statement right before the
// scope_generations delete locks the pruned generations' fact_work_items
// rows, FOR UPDATE (the lock the cascade delete takes) without SKIP LOCKED,
// in work_item_id COLLATE "C" order, for the same generation ids.
func TestGenerationRetentionLocksWorkRowsInWorkItemOrderBeforeDelete(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM fact_work_items AS work",
		"WHERE work.generation_id = ANY($1::text[])",
		`ORDER BY work.work_item_id COLLATE "C"`,
		"FOR UPDATE",
	} {
		if !strings.Contains(lockPrunedGenerationWorkItemsQuery, want) {
			t.Errorf("work-row lock lacks %q:\n%s", want, lockPrunedGenerationWorkItemsQuery)
		}
	}
	for _, unwanted := range []string{"SKIP LOCKED", "NO KEY UPDATE"} {
		if strings.Contains(lockPrunedGenerationWorkItemsQuery, unwanted) {
			t.Errorf("work-row lock must not use %q:\n%s", unwanted, lockPrunedGenerationWorkItemsQuery)
		}
	}

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-a", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
			{"scope-b", "generation-b", "repository", now.Add(-10 * 24 * time.Hour), now.Add(-11 * 24 * time.Hour), false},
		},
		countRows: [][]any{
			{"generation-a", "fact_records", int64(3)},
			{"generation-b", "fact_records", int64(4)},
		},
		execResults: generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2),
		factKeyRows: factKeyTestRows,
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	if _, err := store.PruneSupersededGenerations(context.Background(), factKeyTestPolicy()); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	lockAt := slices.IndexFunc(database.execs, func(call fakeExecCall) bool {
		return call.query == lockPrunedGenerationWorkItemsQuery
	})
	deleteAt := slices.IndexFunc(database.execs, func(call fakeExecCall) bool {
		return call.query == deleteScopeGenerationsForRetentionQuery
	})
	if lockAt < 0 || deleteAt != lockAt+1 {
		t.Fatalf("work-row lock at exec %d and scope_generations delete at %d; want the lock right before the delete", lockAt, deleteAt)
	}
	lockArgs, deleteArgs := database.execs[lockAt].args, database.execs[deleteAt].args
	if len(lockArgs) != 1 || len(deleteArgs) != 1 {
		t.Fatalf("lock args %v, delete args %v; want one generation-id array each", lockArgs, deleteArgs)
	}
	lockIDs, ok := lockArgs[0].([]string)
	if !ok || !slices.Equal(lockIDs, deleteArgs[0].([]string)) {
		t.Fatalf("lock generation ids %v, delete generation ids %v; want the same set", lockArgs[0], deleteArgs[0])
	}
}
