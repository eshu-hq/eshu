// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestReducerQueueReplayWorkloadMaterializationAcceptsConcurrentScheduler(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{},
			rowsAffectedResult{},
		},
		queryResponses: []queueFakeRows{{}, {rows: [][]any{{"pending"}}}},
	}
	queue := ReducerQueue{database: db}

	replayed, err := queue.ReplayWorkloadMaterialization(
		context.Background(),
		"scope-1",
		"gen-1",
		"repo:service-gha",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayWorkloadMaterialization() replayed = false, want true for concurrent replay scheduler")
	}
}

func TestReducerQueueReplayWorkloadMaterializationForFenceRetriesConcurrentInsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
		// Update miss, enqueue conflict, one update retry that schedules.
		queryResponses: []queueFakeRows{{}, {rows: [][]any{{true}}}},
	}
	queue := ReducerQueue{database: db}

	replayed, err := queue.ReplayWorkloadMaterializationForFence(
		context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
	)
	if err != nil || !replayed {
		t.Fatalf("ReplayWorkloadMaterializationForFence() = (%v, %v), want (true, nil)", replayed, err)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("query count = %d, want %d (schedule, held-work-item probe, retry)", got, want)
	}
	if got := len(db.execs); got != 1 {
		t.Fatalf("exec count = %d, want 1: only the conflicted enqueue", got)
	}
	for _, want := range []string{"jsonb_set", "cross_scope_replay_required", "<> $3", "AS requeued"} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("fenced replay update missing %q:\n%s", want, db.queries[0].query)
		}
	}
	if db.queries[1].query != heldWorkItemsQuery {
		t.Fatalf("second query = %q, want the held-work-item probe", db.queries[1].query)
	}
}

func TestReducerQueueReplayWorkloadMaterializationForFenceRejectsTerminalWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{},
			rowsAffectedResult{},
			rowsAffectedResult{},
		},
		queryResponses: []queueFakeRows{{}, {}, {rows: [][]any{{"dead_letter"}}}},
	}
	queue := ReducerQueue{database: db}

	replayed, err := queue.ReplayWorkloadMaterializationForFence(
		context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterializationForFence() error = %v, want nil", err)
	}
	if replayed {
		t.Fatal("ReplayWorkloadMaterializationForFence() replayed = true, want false for terminal work")
	}
}

func TestReducerQueueReplayWorkloadMaterializationRejectsTerminalWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{},
			rowsAffectedResult{},
		},
		queryResponses: []queueFakeRows{{}, {rows: [][]any{{"dead_letter"}}}},
	}
	queue := ReducerQueue{database: db}

	replayed, err := queue.ReplayWorkloadMaterialization(
		context.Background(),
		"scope-1",
		"gen-1",
		"repo:service-gha",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
	}
	if replayed {
		t.Fatal("ReplayWorkloadMaterialization() replayed = true, want false for terminal work")
	}
}
