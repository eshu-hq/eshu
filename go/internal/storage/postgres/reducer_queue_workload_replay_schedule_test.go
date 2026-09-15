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
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	queue := ReducerQueue{db: db}

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

	db := &fakeExecQueryer{execResults: []sql.Result{
		rowsAffectedResult{},
		rowsAffectedResult{},
		rowsAffectedResult{rowsAffected: 1},
	}}
	queue := ReducerQueue{db: db}

	replayed, err := queue.ReplayWorkloadMaterializationForFence(
		context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
	)
	if err != nil || !replayed {
		t.Fatalf("ReplayWorkloadMaterializationForFence() = (%v, %v), want (true, nil)", replayed, err)
	}
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for _, want := range []string{"jsonb_set", "cross_scope_replay_required", "<> $3"} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("fenced replay update missing %q:\n%s", want, db.execs[0].query)
		}
	}
}

func TestReducerQueueReplayWorkloadMaterializationForFenceRejectsTerminalWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{
		rowsAffectedResult{},
		rowsAffectedResult{},
		rowsAffectedResult{},
	}}
	queue := ReducerQueue{db: db}

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
		queryResponses: []queueFakeRows{{rows: [][]any{{false}}}},
	}
	queue := ReducerQueue{db: db}

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
