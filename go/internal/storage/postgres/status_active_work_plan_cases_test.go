// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// summaryPlanText assembles fixed EXPLAIN ANALYZE text shaped like a real
// activeWorkSummaryQuery plan. detail and history are the node lines under
// each CTE header, already indented as PostgreSQL prints them; tail is one
// node under the top-level Append.
func summaryPlanText(detail, history []string, tail string) string {
	lines := []string{"Sort  (cost=10.00..10.01 rows=1 width=96) (actual time=9.000..9.001 rows=40.00 loops=1)"}
	lines = append(lines, "  CTE active_fact_work_items")
	lines = append(lines, detail...)
	lines = append(lines, "  CTE fact_work_history_counts")
	lines = append(lines, history...)
	lines = append(lines,
		"  ->  Append  (cost=0.00..5.00 rows=40 width=96) (actual time=0.100..8.000 rows=40.00 loops=1)",
		"        ->  "+tail,
		"Planning Time: 1.000 ms",
		"Execution Time: 9.500 ms",
	)
	return strings.Join(lines, "\n")
}

const (
	planFullScan  = "Seq Scan on scope_generations stale_generation  (cost=0.00..1834.00 rows=100000 width=48) (actual time=0.010..4.300 rows=100000.00 loops=1)"
	planProbe     = "Index Scan using scope_generations_pkey on scope_generations stale_generation  (cost=0.29..0.31 rows=1 width=48) (actual time=0.001..0.001 rows=1.00 loops=2000)"
	planOtherScan = "Seq Scan on ingestion_scopes scope  (cost=0.00..40.00 rows=2000 width=64) (actual time=0.005..0.200 rows=2000.00 loops=1)"
	planHashJoin  = "Hash Left Join  (cost=2000.00..2600.00 rows=2000 width=88) (actual time=13.000..14.000 rows=2000.00 loops=1)"
	planNestLoop  = "Nested Loop Left Join  (cost=0.00..90000.00 rows=2000 width=88) (actual time=0.100..900.000 rows=2000.00 loops=1)"
	planWorkScan  = "Seq Scan on fact_work_items work  (cost=0.00..400.00 rows=20000 width=96) (actual time=0.005..5.000 rows=20000.00 loops=1)"
)

// planUnder renders one scope_generations subtree under join, with a
// fact_work_items scan as the other input.
func planUnder(join string, inner ...string) []string {
	return append([]string{
		"    ->  " + join,
		"          Join Filter: (stale_generation.scope_id = work.scope_id)",
		"          ->  " + planWorkScan,
	}, inner...)
}

// planHashBuild is Hash Join -> Hash -> Seq Scan (build side).
func planHashBuild(hashLoops, scanLoops string) []string {
	return planUnder(planHashJoin,
		"          ->  Hash  (cost=1834.00..1834.00 rows=100000 width=48) (actual time=9.000..9.000 rows=100000.00 loops="+hashLoops+")",
		"                Buckets: 131072  Batches: 1  Memory Usage: 8000kB",
		"                ->  Seq Scan on scope_generations stale_generation  (cost=0.00..1834.00 rows=100000 width=48) (actual time=0.010..4.300 rows=100000.00 loops="+scanLoops+")",
	)
}

// planHashProbe is the shape PostgreSQL 18.3 chose for the history join on
// the live #4446 fixture: the hash join builds on the groups and reads one
// full scope_generations pass as its outer (probe) side.
func planHashProbe(loops string) []string {
	return []string{
		"    ->  Hash Join  (cost=175.78..3846.79 rows=1 width=72) (actual time=20.762..21.336 rows=2000.00 loops=" + loops + ")",
		"          Hash Cond: ((stale_generation.scope_id = scope_state.scope_id) AND (stale_generation.generation_id = status_group.generation_id))",
		"          ->  Seq Scan on scope_generations stale_generation  (cost=0.00..2921.00 rows=100000 width=54) (actual time=0.006..4.012 rows=100000.00 loops=" + loops + ")",
		"          ->  Hash  (cost=146.68..146.68 rows=1940 width=168) (actual time=9.728..9.729 rows=2000.00 loops=1)",
		"                ->  CTE Scan on fact_work_status_groups status_group  (cost=0.00..55.00 rows=1940 width=136) (actual time=5.754..8.853 rows=2000.00 loops=1)",
	}
}

// planParallelHash is Gather -> Parallel Hash Join -> Parallel Hash ->
// Parallel Seq Scan with two launched workers.
func planParallelHash(loops string) []string {
	return []string{
		"    ->  Gather  (cost=1000.00..3000.00 rows=2000 width=88) (actual time=14.000..15.000 rows=2000.00 loops=1)",
		"          Workers Planned: 2",
		"          Workers Launched: 2",
		"          ->  Parallel Hash Left Join  (cost=2000.00..2600.00 rows=700 width=88) (actual time=13.000..14.000 rows=667.00 loops=3)",
		"                ->  Parallel Seq Scan on fact_work_items work  (cost=0.00..300.00 rows=8333 width=96) (actual time=0.005..2.000 rows=6667.00 loops=3)",
		"                ->  Parallel Hash  (cost=1500.00..1500.00 rows=41667 width=48) (actual time=9.000..9.000 rows=33333.00 loops=" + loops + ")",
		"                      ->  Parallel Seq Scan on scope_generations stale_generation  (cost=0.00..1500.00 rows=41667 width=48) (actual time=0.010..4.300 rows=33333.00 loops=" + loops + ")",
	}
}

var (
	// planMergeSort is Merge Join -> Sort -> Seq Scan, sorted once.
	planMergeSort = planUnder("Merge Left Join  (cost=9000.00..9500.00 rows=2000 width=88) (actual time=40.000..44.000 rows=2000.00 loops=1)",
		"          ->  Sort  (cost=9000.00..9250.00 rows=100000 width=48) (actual time=30.000..35.000 rows=100000.00 loops=1)",
		"                Sort Key: stale_generation.generation_id",
		"                ->  "+planFullScan,
	)
	// planNestedRescan is Nested Loop -> Seq Scan rescanned per outer row.
	planNestedRescan = planUnder(planNestLoop,
		"          ->  Seq Scan on scope_generations stale_generation  (cost=0.00..1834.00 rows=100000 width=48) (actual time=0.010..4.300 rows=1.00 loops=2000)",
	)
	// planNestedMaterialize is Nested Loop -> Materialize -> Seq Scan.
	planNestedMaterialize = planUnder(planNestLoop,
		"          ->  Materialize  (cost=0.00..2334.00 rows=100000 width=48) (actual time=0.001..20.000 rows=100000.00 loops=2000)",
		"                ->  "+planFullScan,
	)
	// planNestedMemoize is Nested Loop -> Memoize -> Seq Scan.
	planNestedMemoize = planUnder(planNestLoop,
		"          ->  Memoize  (cost=0.30..2.32 rows=1 width=48) (actual time=0.001..0.001 rows=1.00 loops=2000)",
		"                Cache Key: work.generation_id",
		"                ->  "+planFullScan,
	)
	// planProbed is a nested loop over pkey probes: no full scan at all.
	planProbed = planUnder(planNestLoop, "          ->  "+planProbe)
)

// TestCheckSummaryGenerationScansRejectsDetailFullScan is the seeded
// RED/GREEN set for checkSummaryGenerationScans on fixed plan text (#7009
// arbiter ruling D1, rules R1-R5). A full scope_generations scan passes only
// as one single pass per CTE under a hash, sort, or gather parent, with loops
// at most Workers Launched + 1; nested-loop, materialize, and memoize rescans
// are the O(rows x generations) class #4446 guards against.
func TestCheckSummaryGenerationScansRejectsDetailFullScan(t *testing.T) {
	t.Parallel()

	twoScans := append(append([]string{}, planHashBuild("1", "1")...), planHashBuild("1", "1")[3:]...)
	for name, tc := range map[string]struct {
		plan string
		want []string // nil accepts; otherwise substrings the error must carry
	}{
		"probes everywhere":                 {summaryPlanText(planProbed, planProbed, planOtherScan), nil},
		"hash build side in both CTEs":      {summaryPlanText(planHashBuild("1", "1"), planHashBuild("1", "1"), planOtherScan), nil},
		"hash join outer side (live shape)": {summaryPlanText(planProbed, planHashProbe("1"), planOtherScan), nil},
		"parallel hash once per process":    {summaryPlanText(planProbed, planParallelHash("3"), planOtherScan), nil},
		"sort under a merge join":           {summaryPlanText(planMergeSort, planProbed, planOtherScan), nil},
		"plain explain without actuals":     {stripActuals(summaryPlanText(planHashBuild("1", "1"), planHashProbe("1"), planOtherScan)), nil},
		"no detail CTE in the plan":         {"Sort\n  ->  " + planFullScan, []string{"no active_fact_work_items CTE"}},
		"history nested loop rescan": {summaryPlanText(planProbed, planNestedRescan, planOtherScan), []string{
			`fact_work_history_counts: full scope_generations scan under "Nested Loop Left Join"`, "scan loops=2000, want at most 1",
		}},
		"history nested loop materialize": {summaryPlanText(planProbed, planNestedMaterialize, planOtherScan), []string{
			`under "Materialize"`, `parent "Materialize" loops=2000, want at most 1`,
		}},
		"detail nested loop memoize": {summaryPlanText(planNestedMemoize, planProbed, planOtherScan), []string{
			`active_fact_work_items: full scope_generations scan under "Memoize"`,
		}},
		"hash rebuilt per outer row": {summaryPlanText(planProbed, planHashBuild("2000", "2000"), planOtherScan), []string{
			"scan loops=2000, want at most 1", `parent "Hash" loops=2000`,
		}},
		"hash join outer side rescanned": {summaryPlanText(planProbed, planHashProbe("2000"), planOtherScan), []string{
			"scan loops=2000, want at most 1",
		}},
		"parallel hash above workers+1": {summaryPlanText(planProbed, planParallelHash("6"), planOtherScan), []string{
			"scan loops=6, want at most 3",
		}},
		"scan outside both CTEs": {summaryPlanText(planProbed, planProbed, planFullScan), []string{
			"1 full scope_generations scans outside active_fact_work_items and fact_work_history_counts",
		}},
		"two scans in one CTE": {summaryPlanText(planProbed, twoScans, planOtherScan), []string{
			"fact_work_history_counts: 2 full scope_generations scans, want at most 1",
		}},
		"history CTE root is the scan": {summaryPlanText(planProbed, []string{"    ->  " + planFullScan}, planOtherScan), []string{
			`under "CTE fact_work_history_counts"`,
		}},
		"plain explain still rejects a nested loop": {stripActuals(summaryPlanText(planProbed, planNestedRescan, planOtherScan)), []string{
			`under "Nested Loop Left Join"`,
		}},
		"violations in both CTEs are all reported": {summaryPlanText(planNestedMemoize, planNestedMaterialize, planOtherScan), []string{
			`active_fact_work_items: full scope_generations scan under "Memoize"`,
			`fact_work_history_counts: full scope_generations scan under "Materialize"`,
		}},
	} {
		err := checkSummaryGenerationScans(tc.plan)
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: err = %v, want accepted\nplan:\n%s", name, err, tc.plan)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted, want rejection containing %q\nplan:\n%s", name, tc.want, tc.plan)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: err = %v, want it to contain %q", name, err, want)
			}
		}
	}
}

// stripActuals removes every "(actual ...)" group, as plain EXPLAIN prints.
func stripActuals(plan string) string {
	lines := strings.Split(plan, "\n")
	for i, line := range lines {
		if at := strings.Index(line, " (actual "); at >= 0 {
			lines[i] = line[:at]
		}
	}
	return strings.Join(lines, "\n")
}

// planA2vDetailBranch is the PostgreSQL 18.3 EXPLAIN ANALYZE of the a2v
// summary in its detail branch on the #4446 fixture (78% live), trimmed of
// Buffers, Storage, and condition lines. The gate CTE leads, each detail arm
// carries a one-time filter over a gate InitPlan, the grouped-only arm and
// the grouped pass never execute, and both scope_generations scans are
// single hashed passes.
const planA2vDetailBranch = `Sort  (cost=37561.48..37561.67 rows=75 width=72) (actual time=94.614..94.639 rows=9.00 loops=1)
  CTE fact_work_summary_mode
    ->  Result  (cost=32.77..32.78 rows=1 width=1) (actual time=0.117..0.120 rows=1.00 loops=1)
          InitPlan 2
            ->  Aggregate  (cost=32.76..32.77 rows=1 width=4) (actual time=0.115..0.118 rows=1.00 loops=1)
  CTE active_fact_work_items
    ->  Hash Join  (cost=5561.02..13958.75 rows=18 width=369) (actual time=25.184..58.839 rows=20000.00 loops=1)
          ->  Hash Join  (cost=5496.02..13892.04 rows=18 width=505) (actual time=18.577..38.594 rows=20000.00 loops=1)
                ->  Subquery Scan on work  (cost=0.02..2982.04 rows=36000 width=443) (actual time=0.127..6.759 rows=20000.00 loops=1)
                      ->  Append  (cost=0.02..2622.04 rows=36000 width=515) (actual time=0.126..5.614 rows=20000.00 loops=1)
                            ->  Result  (cost=0.02..1296.02 rows=16000 width=515) (actual time=0.120..0.120 rows=0.00 loops=1)
                                  One-Time Filter: (InitPlan 5).col1
                                  InitPlan 5
                                    ->  CTE Scan on fact_work_summary_mode  (cost=0.00..0.02 rows=1 width=1) (actual time=0.117..0.118 rows=1.00 loops=1)
                                  ->  Seq Scan on fact_work_items  (cost=0.02..1296.02 rows=16000 width=515) (never executed)
                            ->  Result  (cost=0.02..1146.02 rows=20000 width=515) (actual time=0.006..4.682 rows=20000.00 loops=1)
                                  One-Time Filter: (NOT (InitPlan 6).col1)
                                  InitPlan 6
                                    ->  CTE Scan on fact_work_summary_mode fact_work_summary_mode_1  (cost=0.00..0.02 rows=1 width=1) (actual time=0.000..0.000 rows=1.00 loops=1)
                                  ->  Seq Scan on fact_work_items fact_work_items_1  (cost=0.02..1146.02 rows=20000 width=515) (actual time=0.004..3.247 rows=20000.00 loops=1)
                ->  Hash  (cost=2921.00..2921.00 rows=100000 width=62) (actual time=18.355..18.355 rows=100000.00 loops=1)
                      ->  Seq Scan on scope_generations stale_generation  (cost=0.00..2921.00 rows=100000 width=62) (actual time=0.004..6.265 rows=100000.00 loops=1)
          ->  Hash  (cost=40.00..40.00 rows=2000 width=104) (actual time=6.592..6.592 rows=2000.00 loops=1)
                ->  CTE Scan on active_fact_work_items_scope_state scope_state  (cost=0.00..40.00 rows=2000 width=104) (actual time=0.045..6.236 rows=2000.00 loops=1)
  CTE fact_work_status_groups
    ->  HashAggregate  (cost=1396.02..1416.02 rows=2000 width=78) (actual time=0.014..0.015 rows=0.00 loops=1)
          InitPlan 8
            ->  CTE Scan on fact_work_summary_mode fact_work_summary_mode_2  (cost=0.00..0.02 rows=1 width=1) (actual time=0.001..0.001 rows=1.00 loops=1)
          ->  Result  (cost=0.00..1146.00 rows=20000 width=70) (actual time=0.002..0.003 rows=0.00 loops=1)
                One-Time Filter: (InitPlan 8).col1
                ->  Seq Scan on fact_work_items fact_work_items_2  (cost=0.00..1146.00 rows=20000 width=70) (never executed)
  CTE fact_work_history_counts
    ->  GroupAggregate  (cost=3846.80..3846.82 rows=1 width=72) (actual time=0.057..0.058 rows=0.00 loops=1)
          ->  Sort  (cost=3846.80..3846.80 rows=1 width=72) (actual time=0.056..0.058 rows=0.00 loops=1)
                ->  Hash Join  (cost=175.78..3846.79 rows=1 width=72) (actual time=0.032..0.034 rows=0.00 loops=1)
                      ->  Seq Scan on scope_generations stale_generation_1  (cost=0.00..2921.00 rows=100000 width=54) (actual time=0.015..0.015 rows=1.00 loops=1)
                      ->  Hash  (cost=146.68..146.68 rows=1940 width=168) (actual time=0.015..0.015 rows=0.00 loops=1)
                            ->  Hash Join  (cost=65.00..146.68 rows=1940 width=168) (actual time=0.014..0.015 rows=0.00 loops=1)
                                  ->  CTE Scan on fact_work_status_groups status_group  (cost=0.00..55.00 rows=1940 width=136) (actual time=0.014..0.014 rows=0.00 loops=1)
                                  ->  Hash  (cost=40.00..40.00 rows=2000 width=32) (never executed)
                                        ->  CTE Scan on active_fact_work_items_scope_state scope_state_1  (cost=0.00..40.00 rows=2000 width=32) (never executed)
  ->  Append  (cost=0.00..5.00 rows=40 width=96) (actual time=0.100..8.000 rows=9.00 loops=1)
Planning Time: 9.000 ms
Execution Time: 95.000 ms`

// TestCheckSummaryGenerationScansAcceptsTheGatedDetailPlan proves the
// CTE-subtree extractor does not mis-scope the leading gate CTE or the gate
// InitPlans (#7009 S5 ruling D5.5), and that the gate-runs-once check reads
// the gate node: a gate re-run per row (loops=2000) is rejected.
func TestCheckSummaryGenerationScansAcceptsTheGatedDetailPlan(t *testing.T) {
	t.Parallel()

	if err := checkSummaryGenerationScans(planA2vDetailBranch); err != nil {
		t.Fatalf("gated detail plan rejected: %v", err)
	}
	if err := checkSummaryGateRunsOnce(planA2vDetailBranch); err != nil {
		t.Fatalf("gated detail plan gate check: %v", err)
	}
	gate := "    ->  Result  (cost=32.77..32.78 rows=1 width=1) (actual time=0.117..0.120 rows=1.00 loops=1)"
	if strings.Count(planA2vDetailBranch, gate) != 1 {
		t.Fatalf("gate node anchor matched %d times", strings.Count(planA2vDetailBranch, gate))
	}
	rescanned := strings.Replace(planA2vDetailBranch, gate, strings.Replace(gate, "loops=1)", "loops=2000)", 1), 1)
	if err := checkSummaryGateRunsOnce(rescanned); err == nil || !strings.Contains(err.Error(), "want actual loops=1") {
		t.Fatalf("gate re-run per row accepted: err = %v", err)
	}
	if err := checkSummaryGateRunsOnce(strings.Replace(planA2vDetailBranch, "  CTE fact_work_summary_mode\n", "", 1)); err == nil {
		t.Fatal("plan without the gate CTE accepted")
	}
}
