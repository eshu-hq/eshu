// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestCrossplaneReplaySchedulesAfterConcurrentInsertConflict(t *testing.T) {
	t.Parallel()
	database := &fakeExecQueryer{
		// No row exists at the first schedule snapshot, a concurrent INSERT
		// wins the stable identity (enqueue conflicts), and the fresh
		// schedule transitions the winner.
		execResults:    []sql.Result{rowsAffectedResult{}},
		queryResponses: []queueFakeRows{{}, {rows: [][]any{{true}}}},
	}
	queue := ReducerQueue{database: database}
	scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(context.Background(), "claim-scope", "claim-gen")
	if err != nil || !scheduled {
		t.Fatalf("replay after insert conflict = (%t, %v), want (true, nil)", scheduled, err)
	}
	if len(database.queries) != 3 {
		t.Fatalf("query count = %d, want schedule, held-work-item probe, schedule retry", len(database.queries))
	}
	if len(database.execs) != 1 {
		t.Fatalf("exec count = %d, want 1: only the conflicted enqueue", len(database.execs))
	}
	if database.queries[0].query != database.queries[2].query ||
		!strings.Contains(database.queries[2].query, "cross_scope_replay_required") {
		t.Fatal("conflict winner did not receive the same atomic replay UPDATE")
	}
	if database.queries[1].query != heldWorkItemsQuery {
		t.Fatalf("second query = %q, want the held-work-item probe", database.queries[1].query)
	}
	if !strings.Contains(database.execs[0].query, "INSERT INTO fact_work_items") {
		t.Fatal("exec statement is not the normal reducer enqueue")
	}
}

func TestCrossplaneReplayRejectsTerminalStableItem(t *testing.T) {
	t.Parallel()
	database := &fakeExecQueryer{
		execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
		queryResponses: []queueFakeRows{{}, {}},
	}
	queue := ReducerQueue{database: database}
	scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(context.Background(), "claim-scope", "claim-gen")
	if err != nil || scheduled {
		t.Fatalf("terminal stable item replay = (%t, %v), want (false, nil)", scheduled, err)
	}
}
