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
// instead of the bounded result set. Split into its own file (alongside
// status_operations_test.go) to keep both files under the repo's 500-line cap.
func TestBuildLiveActivityQueryComputesGenerationStateOnBoundedRowsOnly(t *testing.T) {
	t.Parallel()

	query, _ := buildLiveActivityQuery(501, true, nil, nil)

	ctePos := strings.Index(query, "WITH bounded_activity AS (")
	limitPos := strings.Index(query, "LIMIT $1")
	closeParenPos := strings.Index(query, ")\nSELECT")
	casePos := strings.Index(query, "CASE")
	joinPos := strings.Index(query, "LEFT JOIN scope_generations work_gen")
	activeJoinPos := strings.Index(query, "LEFT JOIN scope_generations active_gen")

	for name, pos := range map[string]int{
		"WITH bounded_activity AS (":             ctePos,
		"LIMIT $1":                               limitPos,
		")\\nSELECT":                             closeParenPos,
		"CASE":                                   casePos,
		"LEFT JOIN scope_generations work_gen":   joinPos,
		"LEFT JOIN scope_generations active_gen": activeJoinPos,
	} {
		if pos < 0 {
			t.Fatalf("buildLiveActivityQuery missing %q:\n%s", name, query)
		}
	}
	if ctePos >= limitPos || limitPos >= closeParenPos || closeParenPos >= casePos || casePos >= joinPos || joinPos >= activeJoinPos {
		t.Fatalf("buildLiveActivityQuery must scan+bound inside the CTE, then annotate generation_state only in the outer SELECT, got:\n%s", query)
	}
	if !strings.Contains(query, "WHEN b.status <> 'retrying' THEN 'active'") {
		t.Fatalf("buildLiveActivityQuery must keep claimed/running rows unconditionally 'active', got:\n%s", query)
	}
	if !strings.Contains(query, "AS generation_state") {
		t.Fatalf("buildLiveActivityQuery must project generation_state, got:\n%s", query)
	}
}
