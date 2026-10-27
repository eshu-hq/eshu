// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// Hermetic companion to workflow_run_reconciliation_factless_live_test.go:
// the factless probe must keep both NOT EXISTS subqueries on the
// (scope_id, generation_id) equality prefix so the planner can use
// fact_records_scope_generation_idx and
// fact_work_items_scope_generation_idx instead of scanning either table.
// A probe that drops a scope equality silently degrades reconcile into a
// full scan on every run tick.
func TestFactlessCompletedCountsQueryUsesScopeGenerationPrefixes(t *testing.T) {
	t.Parallel()

	query := listWorkflowCollectorFactlessCompletedCountsQuery
	for _, want := range []string{
		"FROM workflow_work_items AS item",
		"item.status = 'completed'",
		"FROM fact_records AS fact",
		"fact.scope_id = item.scope_id",
		"fact.generation_id = item.generation_id",
		"FROM fact_work_items AS work",
		"work.scope_id = item.scope_id",
		"work.generation_id = item.generation_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("factless probe query missing %q", want)
		}
	}
}
