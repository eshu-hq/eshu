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

	// The schedule statement reports one boolean per matched row: true when
	// that row transitioned (a succeeded reopen or a newly flagged
	// in-flight row), false when it was already outstanding or already
	// flagged (#11615). Terminal and superseded rows match nothing, so the
	// replay falls through to the enqueue conflict and the status probe.
	tests := []struct {
		name         string
		schedule     [][]any
		probe        [][]any
		wantOutcome  reducer.WorkloadMaterializationReplayOutcome
		wantReplayed bool
	}{
		{name: "pending", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "claimed", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "running", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "retrying", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "succeeded", schedule: [][]any{{true}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "superseded", schedule: nil, probe: [][]any{{"superseded"}}, wantOutcome: reducer.WorkloadMaterializationReplaySuperseded},
		{name: "dead letter", schedule: nil, probe: [][]any{{"dead_letter"}}, wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
		{name: "failed", schedule: nil, probe: [][]any{{"failed"}}, wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			newQueue := func() ReducerQueue {
				queries := []queueFakeRows{{rows: tt.schedule}}
				if tt.probe != nil {
					queries = append(queries, queueFakeRows{rows: tt.probe})
				}
				return ReducerQueue{database: &fakeExecQueryer{
					execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
					queryResponses: queries,
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
		queryResponses: []queueFakeRows{{}, {}},
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

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{true}}}}}
	queue := ReducerQueue{database: db}
	outcome, err := queue.ReplayWorkloadMaterializationOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
		t.Fatalf("outcome = (%q, %v), want (scheduled, nil)", outcome, err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want 1: an updated row needs no enqueue", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0: an updated row needs no enqueue", got)
	}
}

// TestReducerQueueReplayStatusQueryReadsStableItemStatus pins the follow-up
// query to the stable work item and to the status text the outcome maps.
func TestReducerQueueReplayStatusQueryReadsStableItemStatus(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}},
		// The schedule finds no eligible row (empty RETURNING); the enqueue
		// admits nothing, so the held-work-item probe (answered outside the
		// FIFO, zero rows) runs before the status read.
		queryResponses: []queueFakeRows{{}, {rows: [][]any{{"superseded"}}}},
	}
	queue := ReducerQueue{database: db}
	if _, err := queue.ReplayWorkloadMaterializationOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha",
	); err != nil {
		t.Fatalf("ReplayWorkloadMaterializationOutcome() error = %v", err)
	}
	// The enqueue that follows the unmatched update admits no row (the fake
	// reports none), so it runs the #11215 held-work-item probe between the
	// schedule and the status read.
	if got := len(db.queries); got != 3 {
		t.Fatalf("query count = %d, want 3 (schedule, held-work-item probe, then status)", got)
	}
	if !strings.Contains(db.queries[0].query, "AS requeued") {
		t.Fatalf("first query = %q, want the schedule statement", db.queries[0].query)
	}
	if db.queries[1].query != heldWorkItemsQuery {
		t.Fatalf("second query = %q, want the held-work-item probe", db.queries[1].query)
	}
	for _, want := range []string{"SELECT status", "work_item_id = $1", "stage = 'reducer'"} {
		if !strings.Contains(db.queries[2].query, want) {
			t.Fatalf("status query missing %q:\n%s", want, db.queries[2].query)
		}
	}
}

// TestReducerQueueReplayWorkloadMaterializationForFenceOutcome pins the fenced
// twin of the closed outcome: the RUNS_ON readiness replay reports scheduled,
// superseded, or not scheduled exactly like the plain path, so the runner can
// tell a superseded stable item (re-check freshness, #7670) from a
// dead-lettered one (fail closed). The fenced conflict shape is update miss,
// enqueue conflict, one update retry, then the shared status probe.
func TestReducerQueueReplayWorkloadMaterializationForFenceOutcome(t *testing.T) {
	t.Parallel()

	// The fenced schedule reports the same transition boolean as the plain
	// path; the conflict shape is update miss, enqueue conflict, one update
	// retry, then the shared status probe. Already-outstanding rows report
	// matched-but-not-transitioned and schedule at once; terminal rows miss
	// both updates and read their status from the probe (#11615).
	tests := []struct {
		name         string
		schedule     [][]any
		retry        [][]any
		probe        [][]any
		wantOutcome  reducer.WorkloadMaterializationReplayOutcome
		wantReplayed bool
	}{
		{name: "pending", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "claimed", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "running", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "retrying", schedule: [][]any{{false}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "succeeded", schedule: [][]any{{true}}, probe: nil, wantOutcome: reducer.WorkloadMaterializationReplayScheduled, wantReplayed: true},
		{name: "superseded", schedule: nil, probe: [][]any{{"superseded"}}, wantOutcome: reducer.WorkloadMaterializationReplaySuperseded},
		{name: "dead letter", schedule: nil, probe: [][]any{{"dead_letter"}}, wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
		{name: "failed", schedule: nil, probe: [][]any{{"failed"}}, wantOutcome: reducer.WorkloadMaterializationReplayNotScheduled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			newQueue := func() ReducerQueue {
				// The fenced path issues the schedule twice (initial
				// update, then one retry after the enqueue conflict)
				// before the status probe, so the retry slot is always
				// staged; leftover FIFO entries are harmless.
				queries := []queueFakeRows{{rows: tt.schedule}, {}}
				if tt.probe != nil {
					queries = append(queries, queueFakeRows{rows: tt.probe})
				}
				return ReducerQueue{database: &fakeExecQueryer{
					execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}, rowsAffectedResult{}},
					queryResponses: queries,
				}}
			}

			outcome, err := newQueue().ReplayWorkloadMaterializationForFenceOutcome(
				context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
			)
			if err != nil {
				t.Fatalf("ReplayWorkloadMaterializationForFenceOutcome() error = %v, want nil", err)
			}
			if outcome != tt.wantOutcome {
				t.Fatalf("outcome = %q, want %q", outcome, tt.wantOutcome)
			}

			replayed, err := newQueue().ReplayWorkloadMaterializationForFence(
				context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
			)
			if err != nil {
				t.Fatalf("ReplayWorkloadMaterializationForFence() error = %v, want nil", err)
			}
			if replayed != tt.wantReplayed {
				t.Fatalf("ReplayWorkloadMaterializationForFence() = %v, want %v: the boolean contract must not change", replayed, tt.wantReplayed)
			}
		})
	}
}

func TestReducerQueueReplayWorkloadMaterializationForFenceOutcomeMissingItemIsNotScheduled(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{database: &fakeExecQueryer{
		execResults:    []sql.Result{rowsAffectedResult{}, rowsAffectedResult{}, rowsAffectedResult{}},
		queryResponses: []queueFakeRows{{}, {}, {}},
	}}
	outcome, err := queue.ReplayWorkloadMaterializationForFenceOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayNotScheduled {
		t.Fatalf("outcome = (%q, %v), want (not_scheduled, nil)", outcome, err)
	}
}

func TestReducerQueueReplayWorkloadMaterializationForFenceOutcomeScheduledByUpdate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{true}}}}}
	queue := ReducerQueue{database: db}
	outcome, err := queue.ReplayWorkloadMaterializationForFenceOutcome(
		context.Background(), "scope-1", "gen-1", "repo:service-gha", "repository:service-gha", "fence-1",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
		t.Fatalf("outcome = (%q, %v), want (scheduled, nil)", outcome, err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want 1: an updated row needs no enqueue", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0: an updated row needs no enqueue", got)
	}
}
