// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestSupersedeProjectorObsoleteGenerationsLocksWorkInWorkItemOrder pins the
// #8261 lock step of Ack's obsolete-generation supersede: after the SKIP
// LOCKED fence and generation step, locked_obsolete_work locks the held
// generations' projector rows in work_item_id COLLATE "C" order with a
// waiting FOR NO KEY UPDATE and rechecks their own stage, scope, generation
// guard and status; the work UPDATE touches only those rows and keeps every
// generation guard.
func TestSupersedeProjectorObsoleteGenerationsLocksWorkInWorkItemOrder(t *testing.T) {
	t.Parallel()
	query := supersedeProjectorObsoleteGenerationsQuery
	generations := strings.Index(query, "locked_obsolete_stale_generations AS (")
	start := strings.Index(query, "locked_obsolete_work AS (")
	update := strings.Index(query, "superseded_work AS (")
	if generations < 0 || start < generations || update < start {
		t.Fatalf("want the generation lock step, then locked_obsolete_work, then the work UPDATE:\n%s", query)
	}
	step := query[start:update]
	for _, want := range []string{
		"JOIN locked_obsolete_stale_generations AS locked",
		"stale.stage = 'projector'",
		"stale.scope_id = $2",
		"stale.generation_id <> $3",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		`ORDER BY stale.work_item_id COLLATE "C"`,
		"FOR NO KEY UPDATE OF stale",
	} {
		if !strings.Contains(step, want) {
			t.Errorf("work lock step lacks %q:\n%s", want, step)
		}
	}
	if strings.Contains(step, "SKIP LOCKED") || strings.Contains(step, "NOWAIT") {
		t.Errorf("work lock step must wait in order, not skip:\n%s", step)
	}
	tail := query[update:]
	for _, want := range []string{
		"stale.work_item_id = ANY(ARRAY(SELECT work_item_id FROM locked_obsolete_work))",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.projection_write_started_at IS NULL",
		"current_generation.status IN ('pending', 'active')",
	} {
		if !strings.Contains(tail, want) {
			t.Errorf("work UPDATE lacks %q:\n%s", want, tail)
		}
	}
}
