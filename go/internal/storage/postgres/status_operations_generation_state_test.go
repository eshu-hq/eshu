// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestBuildLiveActivityQueryComputesGenerationStateOnBoundedRowsOnly is the
// #5138 fix's core shape assertion: the WHERE/ORDER BY/LIMIT scan happens
// inside a bounded_activity CTE (so it stays byte-identical to the pre-fix
// plan -- proven by the EXPLAIN evidence in buildLiveActivityQuery's doc),
// and the generation_state CASE plus its two scope_generations LEFT JOINs
// only appear in the outer SELECT that runs after the CTE's LIMIT, never
// inside the CTE itself where they would scale with total in-flight rows
// instead of the bounded result set. It also pins the cold-review P1 fix: the
// outer SELECT re-asserts its own ORDER BY after both LEFT JOINs, because SQL
// does not guarantee the inner CTE's sorted row order survives an outer
// SELECT with two more joins layered on top -- an unordered outer result
// would silently break both the OpenAPI "most-recently-updated first"
// contract and which row readLiveActivity's limit+1 truncation trim drops.
// Split into its own file (alongside status_operations_test.go) to keep both
// files under the repo's 500-line cap.
func TestBuildLiveActivityQueryComputesGenerationStateOnBoundedRowsOnly(t *testing.T) {
	t.Parallel()

	query, _ := buildLiveActivityQuery(501, true, nil, nil)

	ctePos := strings.Index(query, "WITH bounded_activity AS (")
	limitPos := strings.Index(query, "LIMIT $1")
	closeParenPos := strings.Index(query, ")\nSELECT")
	casePos := strings.Index(query, "CASE")
	joinPos := strings.Index(query, "LEFT JOIN scope_generations work_gen")
	activeJoinPos := strings.Index(query, "LEFT JOIN scope_generations active_gen")
	outerOrderByPos := strings.Index(query, "ORDER BY b.updated_at DESC, b.work_item_id")

	for name, pos := range map[string]int{
		"WITH bounded_activity AS (":                 ctePos,
		"LIMIT $1":                                   limitPos,
		")\\nSELECT":                                 closeParenPos,
		"CASE":                                       casePos,
		"LEFT JOIN scope_generations work_gen":       joinPos,
		"LEFT JOIN scope_generations active_gen":     activeJoinPos,
		"ORDER BY b.updated_at DESC, b.work_item_id": outerOrderByPos,
	} {
		if pos < 0 {
			t.Fatalf("buildLiveActivityQuery missing %q:\n%s", name, query)
		}
	}
	if ctePos >= limitPos || limitPos >= closeParenPos || closeParenPos >= casePos || casePos >= joinPos || joinPos >= activeJoinPos {
		t.Fatalf("buildLiveActivityQuery must scan+bound inside the CTE, then annotate generation_state only in the outer SELECT, got:\n%s", query)
	}
	if outerOrderByPos <= activeJoinPos {
		t.Fatalf("buildLiveActivityQuery must re-assert ORDER BY in the OUTER select, after both LEFT JOINs -- an outer join gives no ordering guarantee from the inner CTE's own ORDER BY, got:\n%s", query)
	}
	if !strings.Contains(query, "WHEN b.status <> 'retrying' THEN 'active'") {
		t.Fatalf("buildLiveActivityQuery must keep claimed/running rows unconditionally 'active', got:\n%s", query)
	}
	if !strings.Contains(query, "AS generation_state") {
		t.Fatalf("buildLiveActivityQuery must project generation_state, got:\n%s", query)
	}
}
