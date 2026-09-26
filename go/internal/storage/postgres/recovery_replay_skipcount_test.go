// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// replayRows builds the single-statement replay response: one row per replayed
// work item, each carrying the superseded-generation skip count. A replay that
// moved nothing still returns one row with a NULL work_item_id so the count is
// never lost (#7130).
func replayRows(skipped int, ids ...string) queueFakeRows {
	if len(ids) == 0 {
		return queueFakeRows{rows: [][]any{{sql.NullString{}, skipped}}}
	}
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []any{sql.NullString{String: id, Valid: true}, skipped})
	}
	return queueFakeRows{rows: rows}
}

// TestReplayFailedWorkItemsReportsSkipsFromTheReplayStatement pins the #7130
// review fix: the superseded-generation skip count travels in the replay
// statement itself. A separate follow-up count query could fail after the
// replay committed, and the operator would see an error for a replay that had
// already reset rows to pending.
func TestReplayFailedWorkItemsReportsSkipsFromTheReplayStatement(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{0, 5} {
		db := &fakeExecQueryer{queryResponses: []queueFakeRows{replayRows(4, "item-1", "item-2")}}
		store := NewRecoveryStore(db)

		result, err := store.ReplayFailedWorkItems(context.Background(),
			recovery.ReplayFilter{Stage: recovery.StageProjector, Limit: limit},
			time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("limit %d: ReplayFailedWorkItems() error = %v, want nil", limit, err)
		}
		if len(db.queries) != 1 {
			t.Fatalf("limit %d: query count = %d, want 1 (replay and skip count in one statement)",
				limit, len(db.queries))
		}
		if !slices.Equal(result.WorkItemIDs, []string{"item-1", "item-2"}) || result.Replayed != 2 {
			t.Fatalf("limit %d: replayed = %v (%d), want [item-1 item-2]", limit, result.WorkItemIDs, result.Replayed)
		}
		if result.SkippedSupersededGeneration != 4 {
			t.Fatalf("limit %d: skipped = %d, want 4", limit, result.SkippedSupersededGeneration)
		}
	}
}

// TestReplayFailedWorkItemsKeepsSkipCountWhenNothingReplays proves the NULL-id
// row carries the count when every matching row is fenced.
func TestReplayFailedWorkItemsKeepsSkipCountWhenNothingReplays(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{replayRows(3)}}
	result, err := NewRecoveryStore(db).ReplayFailedWorkItems(context.Background(),
		recovery.ReplayFilter{Stage: recovery.StageProjector}, time.Now())
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if result.Replayed != 0 || len(result.WorkItemIDs) != 0 || result.SkippedSupersededGeneration != 3 {
		t.Fatalf("result = %+v, want nothing replayed and 3 skipped", result)
	}
}

// TestReplayFailedWorkItemsSkipCountSharesReplayArguments proves the skip
// count reuses the replay's own placeholders: it adds no argument, so the
// bounded and unbounded predicates keep their numbering.
func TestReplayFailedWorkItemsSkipCountSharesReplayArguments(t *testing.T) {
	t.Parallel()

	filter := recovery.ReplayFilter{
		Stage: recovery.StageProjector, ScopeIDs: []string{"scope-1"}, FailureClass: "retry_exhausted",
	}
	for _, limit := range []int{0, 7} {
		filter.Limit = limit
		query, args := buildReplayFailedWorkItemsQuery(filter, time.Now())
		wantArgs := 4 // timestamp, stage, scope ids, failure class
		if limit > 0 {
			wantArgs++ // and the row limit
		}
		if len(args) != wantArgs {
			t.Fatalf("limit %d: args = %d, want %d", limit, len(args), wantArgs)
		}
		if strings.Count(query, "fenced_generation.status = 'superseded'") != 2 {
			t.Fatalf("limit %d: want the fence once negated in the replay and once counted:\n%s", limit, query)
		}
	}
}

// TestReplayFailedWorkItemsNonProjectorStageSkipsNothingAtNoCost proves a
// reducer replay carries a constant zero rather than a scan of fact_work_items
// for fenced rows.
func TestReplayFailedWorkItemsNonProjectorStageSkipsNothingAtNoCost(t *testing.T) {
	t.Parallel()

	query, _ := buildReplayFailedWorkItemsQuery(recovery.ReplayFilter{Stage: recovery.StageReducer}, time.Now())
	// The replay predicate still carries the fence (short-circuited by stage);
	// the skipped CTE must not scan for fenced rows.
	if strings.Count(query, "fenced_generation.status = 'superseded'") != 1 ||
		strings.Contains(query, "COUNT(*)") {
		t.Fatalf("reducer replay must not count fenced rows:\n%s", query)
	}
	if !strings.Contains(query, "0::bigint") {
		t.Fatalf("reducer replay must report a constant zero skip count:\n%s", query)
	}
}
