// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"regexp"
	"strings"
	"testing"
)

// adminLockOrderPattern captures the ORDER BY that sets a row lock's order.
var adminLockOrderPattern = regexp.MustCompile(`(?s)ORDER BY ([^\n]+)\n\s*FOR (?:NO KEY )?UPDATE`)

// TestBuildMutatingWorkItemsQueryLocksSelectedRowsInWorkItemOrder pins the
// #8205 shape of both admin mutations (replay and dead-letter): the shipped
// lock step follows the unlocked LIMIT selection and precedes the UPDATE, it
// locks only still-terminal selected rows in work_item_id COLLATE "C" order
// with a waiting FOR NO KEY UPDATE, and the UPDATE joins through the locked
// rows and rechecks terminal status.
func TestBuildMutatingWorkItemsQueryLocksSelectedRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()
	orders := adminLockOrderPattern.FindAllStringSubmatch(lockSelectedWorkItemsCTE, -1)
	if len(orders) != 1 || strings.TrimSpace(orders[0][1]) != `work_item_id COLLATE "C"` {
		t.Fatalf("lock step order = %q, want exactly work_item_id COLLATE \"C\":\n%s", orders, lockSelectedWorkItemsCTE)
	}
	for _, want := range []string{
		"WHERE work_item_id IN (SELECT work_item_id FROM selected)",
		"AND status IN ('dead_letter', 'failed')",
	} {
		if !strings.Contains(lockSelectedWorkItemsCTE, want) {
			t.Fatalf("lock step lacks %q:\n%s", want, lockSelectedWorkItemsCTE)
		}
	}
	if strings.Contains(lockSelectedWorkItemsCTE, "SKIP LOCKED") || strings.Contains(lockSelectedWorkItemsCTE, "NOWAIT") {
		t.Fatalf("admin lock step must wait in order, not skip:\n%s", lockSelectedWorkItemsCTE)
	}

	for name, query := range map[string]string{
		"replay":      first(buildMutatingWorkItemsQuery(nil, "scope-1", "reducer", "retry_exhausted", 10, 1, true, true, "SET status = 'pending'\n", "manual_review")),
		"dead letter": first(buildMutatingWorkItemsQuery([]string{"wi-1"}, "", "", "", 10, 2, false, false, "SET status = 'dead_letter'\n")),
	} {
		selectionLimit := strings.Index(query, "ORDER BY updated_at DESC, work_item_id ASC LIMIT ")
		lockStep := strings.Index(query, lockSelectedWorkItemsCTE)
		update := strings.Index(query, "UPDATE fact_work_items AS work")
		if selectionLimit < 0 || lockStep < selectionLimit || update < lockStep {
			t.Fatalf("%s: want selection LIMIT, then the shipped lock step, then the UPDATE:\n%s", name, query)
		}
		if strings.Count(query, "FOR NO KEY UPDATE") != 1 || strings.Contains(query, "FOR UPDATE") {
			t.Fatalf("%s: want exactly one row-lock step:\n%s", name, query)
		}
		const wantJoin = "    FROM locked\n    JOIN selected ON selected.work_item_id = locked.work_item_id\n" +
			"    WHERE work.work_item_id = locked.work_item_id\n      AND work.status IN ('dead_letter', 'failed')\n"
		if !strings.Contains(query[update:], wantJoin) {
			t.Fatalf("%s: UPDATE must join through the locked rows and recheck status:\n%s", name, query)
		}
	}
}

// first returns the query of a (query, args) pair.
func first(query string, _ []any) string { return query }
