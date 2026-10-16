// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// sweepLockStepStart and sweepLockStepEnd bound the lock step of
// supersedeInactiveReducerGenerationsCTE.
const (
	sweepLockStepStart = "superseded_stale_reducer_locked AS ("
	sweepLockStepEnd   = "FOR NO KEY UPDATE OF stale\n),\nsuperseded_stale_reducer_generations AS ("
)

// TestSupersedeSweepLocksStaleRowsInWorkItemOrder pins the #8205 shape of the
// reducer claim-time supersede sweep, derived from the shipped constant: a
// lock step that carries the whole sweep predicate (generation guard and
// readiness gate), sorts on stale.work_item_id COLLATE "C" and takes a waiting
// FOR NO KEY UPDATE OF stale, followed by an UPDATE restricted to the locked
// ids that rechecks stage and status. Both claim statements embed it ahead of
// their candidate selection.
func TestSupersedeSweepLocksStaleRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()
	cte := supersedeInactiveReducerGenerationsCTE
	start, end := strings.Index(cte, sweepLockStepStart), strings.Index(cte, sweepLockStepEnd)
	if start < 0 || end < start {
		t.Fatalf("sweep has no lock step ending %q before its UPDATE:\n%s", sweepLockStepEnd, cte)
	}
	lockStep := cte[start : end+len("FOR NO KEY UPDATE OF stale")]
	if !strings.Contains(lockStep, "WHERE "+supersedeInactiveReducerGenerationsPredicate) {
		t.Errorf("lock step must carry the whole sweep predicate:\n%s", lockStep)
	}
	for _, want := range []string{
		"stale.generation_id <> scope.active_generation_id",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
	} {
		if !strings.Contains(lockStep, want) {
			t.Errorf("lock step lacks %q", want)
		}
	}
	orders := lockOrderPattern.FindAllStringSubmatch(lockStep, -1)
	if len(orders) != 1 || strings.TrimSpace(orders[0][1]) != `stale.work_item_id COLLATE "C"` {
		t.Errorf("sweep lock order = %q, want exactly stale.work_item_id COLLATE \"C\"", orders)
	}
	if strings.Contains(lockStep, "SKIP LOCKED") || strings.Contains(lockStep, "NOWAIT") {
		t.Errorf("sweep lock step must wait in order, not skip:\n%s", lockStep)
	}
	update := cte[end:]
	for _, want := range []string{
		"WHERE stale.work_item_id = ANY(ARRAY(SELECT work_item_id FROM superseded_stale_reducer_locked))",
		"AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
	} {
		if !strings.Contains(update, want) {
			t.Errorf("sweep UPDATE lacks %q:\n%s", want, update)
		}
	}
	for name, query := range map[string]string{
		"claimReducerWorkQuery":      claimReducerWorkQuery,
		"claimReducerWorkBatchQuery": claimReducerWorkBatchQuery,
	} {
		if !strings.Contains(query, cte) {
			t.Errorf("%s does not embed the ordered supersede sweep", name)
		}
	}
}
