// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "testing"

// TestPlanNodeIndexCondIsBoundedToItsOwnNode pins the plan-guard parser: the Index
// Cond it returns belongs to the node that scans the named index, and a later
// node's condition is never taken for it.
func TestPlanNodeIndexCondIsBoundedToItsOwnNode(t *testing.T) {
	t.Parallel()

	plan := `Limit
  ->  Nested Loop
        ->  Index Only Scan using zz_cve_idx on supports s_1 (actual time=0.0..0.1 rows=500)
              Heap Fetches: 0
        ->  Index Scan using supports_pkey on supports s (actual time=0.0..0.0 rows=1)
              Index Cond: (fact_id = s_1.fact_id)
        ->  Index Only Scan using zz_pkg_idx on supports s_2 (actual time=0.0..0.1 rows=500)
              Index Cond: ((package_id = k_1.v) AND (fact_id > $5))`
	if cond, found := planNodeIndexCond(plan, "zz_cve_idx"); !found || cond != "" {
		t.Fatalf("cve node: cond = %q, found = %v, want no Index Cond of its own and found (the pkey node's must not be taken)", cond, found)
	}
	if cond, found := planNodeIndexCond(plan, "zz_pkg_idx"); !found || cond != "Index Cond: ((package_id = k_1.v) AND (fact_id > $5))" {
		t.Fatalf("package node: cond = %q, found = %v, want its own Index Cond", cond, found)
	}
	if _, found := planNodeIndexCond(plan, "zz_missing_idx"); found {
		t.Fatal("a missing index reported as found")
	}
	indexes := map[string]string{"package": "zz_pkg_idx"}
	if why := planGuardViolation(plan, indexes, true); why != "" {
		t.Fatalf("a plan with the key and cursor in the node's Index Cond violates the guard: %s", why)
	}
	keyless := `Limit
  ->  Index Only Scan using zz_pkg_idx on supports s_2 (actual time=0.0..0.1 rows=500)
        Index Cond: (fact_id > $5)`
	if why := planGuardViolation(keyless, indexes, true); why == "" {
		t.Fatal("the guard passed a plan whose Index Cond lacks the key")
	}
}
