// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// blockagePlanFixture is an EXPLAIN (ANALYZE, FORMAT JSON) root shaped like the
// summary query's: each CTE is an InitPlan child named "CTE <name>", and
// blocked is inlined into all_blocked. The %s slots take the all_blocked body
// and the unrelated active_fact_work_items body.
const blockagePlanFixture = `{
  "Node Type": "Sort", "Actual Loops": 1, "Plans": [
    {"Node Type": "Hash Join", "Parent Relationship": "InitPlan", "Subplan Name": "CTE active_fact_work_items", "Actual Loops": 1, "Plans": [%s]},
    {"Node Type": "CTE Scan", "Parent Relationship": "InitPlan", "Subplan Name": "CTE eligible", "CTE Name": "active_fact_work_items", "Actual Loops": 1},
    {"Node Type": "Index Scan", "Parent Relationship": "InitPlan", "Subplan Name": "CTE inflight_leases", "Relation Name": "fact_work_items", "Actual Loops": 1},
    {"Node Type": "Append", "Parent Relationship": "InitPlan", "Subplan Name": "CTE all_blocked", "Actual Loops": 1, "Plans": [%s]}
  ]
}`

const (
	// hashedBlockedFilter is blocked as shipped: a hashed SubPlan over the
	// lease CTE filtering the eligible scan.
	hashedBlockedFilter = `{"Node Type": "CTE Scan", "Parent Relationship": "Member", "CTE Name": "eligible", "Actual Loops": 1,
  "Filter": "COALESCE((ANY ((conflict_domain = (hashed SubPlan 5).col1) AND (conflict_key = (hashed SubPlan 5).col2))), false)",
  "Plans": [{"Node Type": "CTE Scan", "Parent Relationship": "SubPlan", "Subplan Name": "SubPlan 5", "CTE Name": "inflight_leases", "Actual Loops": 1}]}`
	// unhashedBlockedFilter is the same filter when the planner refuses to
	// hash the lease set: the lease CTE is rescanned per eligible row.
	unhashedBlockedFilter = `{"Node Type": "CTE Scan", "Parent Relationship": "Member", "CTE Name": "eligible", "Actual Loops": 1,
  "Filter": "COALESCE((ANY ((conflict_domain = (SubPlan 5).col1) AND (conflict_key = (SubPlan 5).col2))), false)",
  "Plans": [{"Node Type": "CTE Scan", "Parent Relationship": "SubPlan", "Subplan Name": "SubPlan 5", "CTE Name": "inflight_leases", "Actual Loops": 40000}]}`
	// flippedBlockedJoin is the old join with stale statistics: leases outer,
	// eligible rescanned once per lease.
	flippedBlockedJoin = `{"Node Type": "Nested Loop", "Parent Relationship": "Member", "Actual Loops": 1, "Plans": [
  {"Node Type": "CTE Scan", "Parent Relationship": "Outer", "CTE Name": "inflight_leases", "Actual Loops": 1},
  {"Node Type": "CTE Scan", "Parent Relationship": "Inner", "CTE Name": "eligible", "Actual Loops": 200}]}`
	// quietUnrelated is an unrelated CTE body that scans fact_work_items once.
	quietUnrelated = `{"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "fact_work_items", "Actual Loops": 1}`
	// noisyUnrelated is an unrelated CTE body with a per-row nested loop over
	// fact_work_items and a hashed SubPlan filter of its own. Neither is the
	// blockage section's, so neither may decide the blockage checks.
	noisyUnrelated = `{"Node Type": "Nested Loop", "Parent Relationship": "Outer", "Actual Loops": 1,
  "Filter": "(hashed SubPlan 9)", "Plans": [
  {"Node Type": "Index Scan", "Parent Relationship": "Inner", "Relation Name": "fact_work_items", "Actual Loops": 5000},
  {"Node Type": "CTE Scan", "Parent Relationship": "SubPlan", "Subplan Name": "SubPlan 9", "CTE Name": "unrelated", "Actual Loops": 1}]}`
)

// TestBlockagePlanChecksReadOnlyTheBlockageSubtree guards the #6794 plan
// regression's helpers (review of PR #6825): they must judge the blockage
// section only, the eligible, inflight_leases, and all_blocked CTEs. A nested
// loop over fact_work_items elsewhere in the summary query must not fail them,
// and a hashed SubPlan elsewhere must not stand in for the blockage filter's.
func TestBlockagePlanChecksReadOnlyTheBlockageSubtree(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		blocked, unrelated string
		wantHashed         bool
		wantLoops          int
	}{
		"shipped filter":                            {hashedBlockedFilter, quietUnrelated, true, 1},
		"shipped filter, noisy unrelated CTE":       {hashedBlockedFilter, noisyUnrelated, true, 1},
		"unhashed filter, hashed SubPlan elsewhere": {unhashedBlockedFilter, noisyUnrelated, false, 40000},
		"flipped join, noisy unrelated CTE":         {flippedBlockedJoin, noisyUnrelated, false, 200},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			plan := decodeBlockagePlanFixture(t, tc.blocked, tc.unrelated)
			if got := planFiltersByHashedSubPlan(plan); got != tc.wantHashed {
				t.Fatalf("hashed blockage filter = %v, want %v", got, tc.wantHashed)
			}
			if got := maxBlockagePathLoops(plan); got != tc.wantLoops {
				t.Fatalf("blockage-path max loops = %d, want %d", got, tc.wantLoops)
			}
		})
	}
}

// decodeBlockagePlanFixture fills blockagePlanFixture and decodes it.
func decodeBlockagePlanFixture(t *testing.T, blocked, unrelated string) map[string]any {
	t.Helper()
	var plan map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprintf(blockagePlanFixture, unrelated, blocked)), &plan); err != nil {
		t.Fatalf("decode plan fixture: %v", err)
	}
	return plan
}

// TestBlockagePlanRootsReportAnInlinedBlockageCTE proves the plan checks
// notice when a blockage CTE is no longer its own InitPlan (the planner inlined
// it), instead of silently reading nothing.
func TestBlockagePlanRootsReportAnInlinedBlockageCTE(t *testing.T) {
	t.Parallel()

	plan := decodeBlockagePlanFixture(t, hashedBlockedFilter, quietUnrelated)
	if _, missing := blockagePlanRoots(plan); len(missing) != 0 {
		t.Fatalf("complete fixture reports missing %v", missing)
	}
	children := plan["Plans"].([]any)
	plan["Plans"] = children[:len(children)-1] // drop the all_blocked InitPlan
	if _, missing := blockagePlanRoots(plan); !slices.Equal(missing, []string{"CTE all_blocked"}) {
		t.Fatalf("missing = %v, want [CTE all_blocked]", missing)
	}
	if planFiltersByHashedSubPlan(plan) {
		t.Fatal("hashed blockage filter reported without an all_blocked subtree")
	}
}
