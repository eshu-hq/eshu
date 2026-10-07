// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerQueueReplayWorkloadMaterializationOutcome pins the closed outcome
// the repo-dependency runner uses to tell a superseded stable item (nothing to
// replay, #7670) from every other unscheduled replay, which must keep failing.
func TestReducerQueueReplayWorkloadMaterializationOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       any
		wantOutcome  reducer.WorkloadMaterializationReplayOutcome
		wantReplayed bool
	}{
		{name: "pending", status: "pending", wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "claimed", status: "claimed", wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "running", status: "running", wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "retrying", status: "retrying", wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "succeeded", status: "succeeded", wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "superseded", status: "superseded", wantOutcome: reducer.WorkloadMaterializationReplaySuperseded},
		{name: "dead letter", status: "dead_letter", wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
		{name: "failed", status: "failed", wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			newQueue := func() ReducerQueue {
				return ReducerQueue{database: &fakeExecQueryer{
					execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
					queryResponses: []queueFakeRows{{rows: [][]any{{tt.status}}}},
				}}
			}

			outcome, err := newQueue().ReplayWorkloadMaterializationOutcome(
				context.Background(), "scope-1", "gen-1", "repo:service-gha",
			)
			if err != nil {
				t.Fatalf("ReplayWorkloadMaterializationOutcome() error = %v, want nil", err)
			}
			if outcome != tt.wantOutcome {
				t.Fatalf("outcome = %q, want %q", outcome, tt.wantOutcome)
			}

			replayed, err := newQueue().ReplayWorkloadMaterialization(
				context.Background(), "scope-1", "gen-1", "repo:service-gha",
			)
			if err != nil {
				t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
			}
			if replayed != tt.wantReplayed {
				t.Fatalf("ReplayWorkloadMaterialization() = %v, want %v: the boolean contract must not change", replayed, tt.wantReplayed)
			}
		})
	}
}

func TestReducerQueueReplayWorkloadMaterializationOutcomeMissingItemIsNotScheduled(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{database: &fakeExecQueryer{
		execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
		queryResponses: []queueFakeRows{{}},
	}}
	outcome, err := queue.ReplayWorkloadMaterializationOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayNotScheduled {
		t.Fatalf("outcome = (%q, %v), want (not_scheduled, nil)", outcome, err)
	}
}

func TestReducerQueueReplayWorkloadMaterializationOutcomeScheduledByUpdate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}}}
	queue := ReducerQueue{database: db}
	outcome, err := queue.ReplayWorkloadMaterializationOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
		t.Fatalf("outcome = (%q, %v), want (scheduled, nil)", outcome, err)
	}
	if got := len(db.execs); got != 1 {
		t.Fatalf("exec count = %d, want 1: an updated row needs no enqueue", got)
	}
}

// TestReducerQueueReplayStatusQueryReadsStableItemStatus pins the follow-up
// query to the stable work item and to the status text the outcome maps.
func TestReducerQueueReplayStatusQueryReadsStableItemStatus(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
		queryResponses: []queueFakeRows{{rows: [][]any{{"superseded"}}}},
	}
	queue := ReducerQueue{database: db}
	if _, err := queue.ReplayWorkloadMaterializationOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha",
	); err != nil {
		t.Fatalf("ReplayWorkloadMaterializationOutcome() error = %v", err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want 1", got)
	}
	for _, want := range []string{"SELECT status", "work_item_id = $1", "stage = 'reducer'"} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("status query missing %q:\n%s", want, db.queries[0].query)
		}
	}
}
