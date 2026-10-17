// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison

import (
	"strings"
	"testing"
)

// TestRecoverDeadLettersLocksRowsInWorkItemOrder pins the #8261 lock step of
// RecoverDeadLettersQuery: between the unlocked selection and the UPDATE, a
// poison_locked CTE takes FOR NO KEY UPDATE (blocking, no SKIP LOCKED) in
// work_item_id COLLATE "C" order and rechecks the row's own status and
// recovery budget, and the UPDATE touches only the locked rows and rechecks
// the status itself.
func TestRecoverDeadLettersLocksRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()
	query := RecoverDeadLettersQuery
	start := strings.Index(query, "poison_locked AS (")
	update := strings.Index(query, "UPDATE fact_work_items AS target")
	if start < 0 || update < start {
		t.Fatalf("no poison_locked CTE before the UPDATE:\n%s", query)
	}
	lockStep := query[start:update]
	for _, want := range []string{
		"locked.work_item_id = ANY(ARRAY(SELECT work_item_id FROM poison))",
		"locked.status = 'dead_letter'",
		"COALESCE((locked.payload ->> 'poison_recovery_attempts')::int, 0) < $2",
		`ORDER BY locked.work_item_id COLLATE "C"`,
		"FOR NO KEY UPDATE",
	} {
		if !strings.Contains(lockStep, want) {
			t.Errorf("lock step lacks %q:\n%s", want, lockStep)
		}
	}
	if strings.Contains(lockStep, "SKIP LOCKED") {
		t.Errorf("lock step skips locked rows; it must wait so no eligible dead letter escapes:\n%s", lockStep)
	}
	tail := query[update:]
	for _, want := range []string{
		"WHERE target.work_item_id = ANY(ARRAY(SELECT work_item_id FROM poison_locked))",
		"AND target.status = 'dead_letter'",
	} {
		if !strings.Contains(tail, want) {
			t.Errorf("UPDATE lacks %q:\n%s", want, tail)
		}
	}
}
