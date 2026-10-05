// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
)

// explainCTESubtree returns the text EXPLAIN lines of the named CTE: its
// "CTE <name>" header and every following line indented deeper than it.
func explainCTESubtree(plan, name string) (string, bool) {
	lines := strings.Split(plan, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "CTE "+name {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		end := i + 1
		for end < len(lines) && len(lines[end])-len(strings.TrimLeft(lines[end], " ")) > indent {
			end++
		}
		return strings.Join(lines[i:end], "\n"), true
	}
	return "", false
}

// checkSummaryGenerationScans is the activeWorkSummaryQuery half of the
// #4446 no-full-scan guard. The per-work-row generation join in
// active_fact_work_items must never scan scope_generations in full: that
// cost grows with work rows times generations. Since #7009 the grouped
// history join (fact_work_history_counts) joins once per (scope, generation,
// stage, status) group, and the planner may hash it against one pass over
// scope_generations; that pass is allowed there and nowhere else.
func checkSummaryGenerationScans(plan string) error {
	const fullScan = "Seq Scan on scope_generations"
	detail, ok := explainCTESubtree(plan, "active_fact_work_items")
	if !ok {
		return fmt.Errorf("plan has no active_fact_work_items CTE")
	}
	if strings.Contains(detail, fullScan) {
		return fmt.Errorf("active_fact_work_items scans scope_generations in full:\n%s", detail)
	}
	history, _ := explainCTESubtree(plan, "fact_work_history_counts")
	if outside := strings.Count(plan, fullScan) - strings.Count(history, fullScan); outside > 0 {
		return fmt.Errorf("%d full scope_generations scans outside fact_work_history_counts", outside)
	}
	if strings.Count(history, fullScan) > 1 {
		return fmt.Errorf("fact_work_history_counts scans scope_generations more than once:\n%s", history)
	}
	return nil
}

// TestCheckSummaryGenerationScansRejectsDetailFullScan is the seeded
// RED/GREEN pair for checkSummaryGenerationScans on fixed plan text.
func TestCheckSummaryGenerationScansRejectsDetailFullScan(t *testing.T) {
	t.Parallel()

	plan := func(detailScan, historyScan, tailScan string) string {
		return strings.Join([]string{
			"Sort",
			"  CTE active_fact_work_items",
			"    ->  Hash Join",
			"          ->  " + detailScan,
			"  CTE fact_work_history_counts",
			"    ->  Hash Join",
			"          ->  " + historyScan,
			"  ->  Append",
			"        ->  " + tailScan,
		}, "\n")
	}
	const (
		full  = "Seq Scan on scope_generations stale_generation"
		probe = "Index Scan using scope_generations_pkey on scope_generations stale_generation"
		other = "Seq Scan on ingestion_scopes scope"
	)
	for name, tc := range map[string]struct {
		plan    string
		wantErr bool
	}{
		"probes everywhere":          {plan(probe, probe, other), false},
		"history hashes one pass":    {plan(probe, full, other), false},
		"detail scans in full":       {plan(full, probe, other), true},
		"scan outside both CTEs":     {plan(probe, probe, full), true},
		"detail and history in full": {plan(full, full, other), true},
		"no detail CTE in the plan":  {"Sort\n  ->  " + full, true},
	} {
		if err := checkSummaryGenerationScans(tc.plan); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", name, err, tc.wantErr)
		}
	}
}
