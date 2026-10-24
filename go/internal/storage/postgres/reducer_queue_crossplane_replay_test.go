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
	database := &fakeExecQueryer{execResults: []sql.Result{
		rowsAffectedResult{},                // No row existed at the first UPDATE snapshot.
		rowsAffectedResult{},                // A concurrent INSERT won the stable identity.
		rowsAffectedResult{rowsAffected: 1}, // Fresh UPDATE dirties the winner.
	}}
	queue := ReducerQueue{database: database}
	scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(context.Background(), "claim-scope", "claim-gen")
	if err != nil || !scheduled {
		t.Fatalf("replay after insert conflict = (%t, %v), want (true, nil)", scheduled, err)
	}
	if len(database.execs) != 3 {
		t.Fatalf("statement count = %d, want UPDATE, INSERT, UPDATE", len(database.execs))
	}
	if database.execs[0].query != database.execs[2].query ||
		!strings.Contains(database.execs[2].query, "cross_scope_replay_required") {
		t.Fatal("conflict winner did not receive the same atomic replay UPDATE")
	}
	if !strings.Contains(database.execs[1].query, "INSERT INTO fact_work_items") {
		t.Fatal("second statement is not the normal reducer enqueue")
	}
}

func TestCrossplaneReplayRejectsTerminalStableItem(t *testing.T) {
	t.Parallel()
	database := &fakeExecQueryer{execResults: []sql.Result{
		rowsAffectedResult{},
		rowsAffectedResult{},
		rowsAffectedResult{},
	}}
	queue := ReducerQueue{database: database}
	scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(context.Background(), "claim-scope", "claim-gen")
	if err != nil || scheduled {
		t.Fatalf("terminal stable item replay = (%t, %v), want (false, nil)", scheduled, err)
	}
}
