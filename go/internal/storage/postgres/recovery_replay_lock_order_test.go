// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// replayLockStepEnd ends the lock step of replayFailedWorkItemsTemplate: the
// locked CTE closes and the UPDATE CTE opens.
const replayLockStepEnd = "    FOR NO KEY UPDATE\n), replayed AS ("

// shippedReplayLockStep derives the template prefix through the lock step from
// the shipped constant, so the shape tests never hand-copy the SQL.
func shippedReplayLockStep(t *testing.T) string {
	t.Helper()
	end := strings.Index(replayFailedWorkItemsTemplate, replayLockStepEnd)
	if end < 0 {
		t.Fatalf("replayFailedWorkItemsTemplate has no lock step ending %q", replayLockStepEnd)
	}
	return replayFailedWorkItemsTemplate[:end+len(replayLockStepEnd)]
}

// TestReplayFailedWorkItemsLocksRowsInWorkItemOrder pins the #8205 shape of
// every unbounded replay variant: the rendered statement opens with the lock
// step derived from the shipped template, which selects the filter's terminal
// rows ordered by work_item_id COLLATE "C" and takes FOR NO KEY UPDATE (no
// SKIP LOCKED: the unbounded replay must wait, not skip); the UPDATE then
// touches only the locked rows and rechecks their terminal status.
func TestReplayFailedWorkItemsLocksRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()
	lockStep := shippedReplayLockStep(t)
	for name, filter := range map[string]recovery.ReplayFilter{
		"stage only":    {Stage: recovery.StageReducer},
		"scoped class":  {Stage: recovery.StageReducer, ScopeIDs: []string{"s1"}, FailureClass: "retry_exhausted"},
		"drain exclude": {Stage: recovery.StageReducer, ExcludeFailureClasses: []string{"manual_review"}},
		"projector":     {Stage: recovery.StageProjector, ScopeIDs: []string{"s1"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			query, _ := buildReplayFailedWorkItemsQuery(filter, time.Unix(0, 0))
			skipped := replaySkippedNoneCTE
			if filter.Stage == recovery.StageProjector {
				skipped = fmt.Sprintf(replaySkippedProjectorCTE, buildUnfencedReplayPredicate(filter, 2).clause)
			}
			wantPrefix := fmt.Sprintf(lockStep, buildReplayPredicate(filter, 2).clause, skipped)
			if !strings.HasPrefix(query, wantPrefix) {
				t.Fatalf("rendered replay does not open with the shipped lock step:\n got: %s\nwant prefix: %s", query, wantPrefix)
			}
			lockedStart := strings.Index(query, "locked AS ")
			lockedEnd := strings.Index(query, replayLockStepEnd)
			if lockedStart < 0 || lockedEnd < lockedStart {
				t.Fatalf("no locked CTE before the UPDATE:\n%s", query)
			}
			locked := query[lockedStart : lockedEnd+len("    FOR NO KEY UPDATE")]
			if !strings.Contains(locked, "WHERE status IN ('dead_letter', 'failed')") {
				t.Errorf("locked CTE must lock only terminal rows:\n%s", locked)
			}
			orders := lockOrderPattern.FindAllStringSubmatch(locked, -1)
			if len(orders) != 1 || strings.TrimSpace(orders[0][1]) != `work_item_id COLLATE "C"` {
				t.Errorf("locked CTE lock order = %q, want exactly work_item_id COLLATE \"C\":\n%s", orders, locked)
			}
			if strings.Contains(locked, "SKIP LOCKED") || strings.Contains(locked, "NOWAIT") {
				t.Errorf("unbounded replay lock step must wait in order, not skip:\n%s", locked)
			}
			update := query[lockedEnd:]
			const wantUpdateWhere = "    WHERE work_item_id = ANY(ARRAY(SELECT work_item_id FROM locked))\n" +
				"      AND status IN ('dead_letter', 'failed')\n    RETURNING work_item_id"
			if !strings.Contains(update, wantUpdateWhere) {
				t.Errorf("UPDATE must touch only locked rows and recheck terminal status:\n%s", update)
			}
			if strings.Count(query, "FOR NO KEY UPDATE") != 1 || strings.Contains(query, "FOR UPDATE") {
				t.Errorf("unbounded replay must take exactly one row-lock step:\n%s", query)
			}
		})
	}
}

// TestReplayFailedWorkItemsSharesTheBatchAckLockOrder adds the unbounded
// replay to the one reducer lock order (#7597): its lock step sorts on
// work_item_id COLLATE "C", like the batch ack and the own-gate wake, so no
// two of them take shared rows in opposite orders.
func TestReplayFailedWorkItemsSharesTheBatchAckLockOrder(t *testing.T) {
	t.Parallel()
	query, _ := buildReplayFailedWorkItemsQuery(recovery.ReplayFilter{Stage: recovery.StageReducer}, time.Unix(0, 0))
	for name, sql := range map[string]string{
		"replayFailedWorkItemsTemplate":    query,
		"ackReducerWorkBatchQuery":         ackReducerWorkBatchQuery(),
		"wakeProducerTerminalConsumersSQL": wakeProducerTerminalConsumersSQL,
	} {
		orders := lockOrderPattern.FindAllStringSubmatch(sql, -1)
		if len(orders) == 0 {
			t.Fatalf("%s: no ORDER BY before a row lock", name)
		}
		for _, order := range orders {
			if !strings.Contains(order[1], `work_item_id COLLATE "C"`) {
				t.Fatalf("%s locks in order %q, want work_item_id COLLATE \"C\"", name, order[1])
			}
		}
	}
}

// TestReplayFailedWorkItemsBoundedKeepsSkipLocked guards the scope of #8205:
// the bounded drain (Limit > 0) keeps its own SKIP LOCKED lock step and gets
// no second one.
func TestReplayFailedWorkItemsBoundedKeepsSkipLocked(t *testing.T) {
	t.Parallel()
	query, _ := buildReplayFailedWorkItemsQuery(recovery.ReplayFilter{Stage: recovery.StageReducer, Limit: 3}, time.Unix(0, 0))
	if !strings.Contains(query, "FOR UPDATE SKIP LOCKED") || strings.Contains(query, "locked AS") {
		t.Fatalf("bounded replay changed shape:\n%s", query)
	}
}
