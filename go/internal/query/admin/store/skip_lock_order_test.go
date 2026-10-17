// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSkipRepositoryWorkItemsLocksSelectedRowsInWorkItemOrder pins the #8261
// shape of the repository skip: after the unlocked LIMIT selection, one lock
// step locks the selected rows still in their selected state in work_item_id
// COLLATE "C" order with a waiting FOR NO KEY UPDATE, and the UPDATE joins
// only the locked rows (each carrying its selected status) and rechecks the
// selected state itself.
func TestSkipRepositoryWorkItemsLocksSelectedRowsInWorkItemOrder(t *testing.T) {
	t.Parallel()

	database := &scopeResolvingNoteQueryer{}
	s := &postgresStore{database: database, now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	if _, err := s.SkipRepositoryWorkItems(context.Background(), "repo-a", "skip"); err != nil {
		t.Fatalf("SkipRepositoryWorkItems() error = %v", err)
	}
	query := database.query
	selection := strings.Index(query, "LIMIT 100")
	lockStep := strings.Index(query, "locked AS (")
	update := strings.Index(query, "UPDATE fact_work_items AS work")
	if selection < 0 || lockStep < selection || update < lockStep {
		t.Fatalf("want selection LIMIT, then the locked CTE, then the UPDATE:\n%s", query)
	}
	orders := adminLockOrderPattern.FindAllStringSubmatch(query, -1)
	if len(orders) != 1 || strings.TrimSpace(orders[0][1]) != `work.work_item_id COLLATE "C"` {
		t.Fatalf("lock step order = %q, want exactly work.work_item_id COLLATE \"C\":\n%s", orders, query)
	}
	step := query[lockStep:update]
	for _, want := range []string{
		"JOIN selected ON selected.work_item_id = work.work_item_id",
		"WHERE work.status = selected.selected_status",
		"AND work.status IN ('pending', 'retrying', 'failed')",
		"FOR NO KEY UPDATE OF work",
	} {
		if !strings.Contains(step, want) {
			t.Errorf("lock step lacks %q:\n%s", want, step)
		}
	}
	if strings.Contains(query, "SKIP LOCKED") || strings.Contains(query, "NOWAIT") {
		t.Errorf("skip lock step must wait in order, not skip:\n%s", query)
	}
	for _, want := range []string{
		"FROM locked\n    WHERE work.work_item_id = locked.work_item_id",
		"AND work.status = locked.selected_status",
		"AND work.status IN ('pending', 'retrying', 'failed')",
	} {
		if !strings.Contains(query[update:], want) {
			t.Errorf("UPDATE lacks %q:\n%s", want, query[update:])
		}
	}
}
