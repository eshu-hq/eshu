// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// explainCTERange returns the half-open line range [lo, hi) of the named
// CTE's header and subtree.
func explainCTERange(lines []string, name string) (int, int, bool) {
	for i, line := range lines {
		if strings.TrimSpace(line) != "CTE "+name {
			continue
		}
		end := i + 1
		for end < len(lines) && explainIndent(lines[end]) > explainIndent(line) {
			end++
		}
		return i, end, true
	}
	return 0, 0, false
}

// explainIndent is the column where a text EXPLAIN line's content starts.
func explainIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// explainNodeName returns the node name of a text EXPLAIN node line, such as
// "Hash" for "->  Hash  (cost=...)" or "Sort" for a root "Sort  (cost=...)".
func explainNodeName(line string) string {
	body := strings.TrimPrefix(strings.TrimSpace(line), "->  ")
	name, _, _ := strings.Cut(body, "  (")
	return strings.TrimSpace(name)
}

// explainParent returns the index of the nearest shallower line above line
// i (its parent node, or a CTE/InitPlan header), or -1 at the plan root.
func explainParent(lines []string, i int) int {
	for j := i - 1; j >= 0; j-- {
		if strings.TrimSpace(lines[j]) != "" && explainIndent(lines[j]) < explainIndent(lines[i]) {
			return j
		}
	}
	return -1
}

var (
	explainLoopsPattern   = regexp.MustCompile(`\(actual [^)]*\bloops=(\d+)\)`)
	explainWorkersPattern = regexp.MustCompile(`^Workers Launched: (\d+)$`)
)

// explainLoops returns the actual loops of a node line, if it has actuals.
func explainLoops(line string) (int, bool) {
	m := explainLoopsPattern.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// explainLoopLimit is Workers Launched + 1 of the nearest Gather or Gather
// Merge enclosing line i, and 1 when no Gather encloses it (or the Gather
// reports no launched workers).
func explainLoopLimit(lines []string, i int) int {
	for j := explainParent(lines, i); j >= 0; j = explainParent(lines, j) {
		if name := explainNodeName(lines[j]); name != "Gather" && name != "Gather Merge" {
			continue
		}
		for k := j + 1; k < len(lines) && explainIndent(lines[k]) > explainIndent(lines[j]); k++ {
			detail := strings.TrimSpace(lines[k])
			if strings.HasPrefix(detail, "->") {
				break
			}
			if m := explainWorkersPattern.FindStringSubmatch(detail); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					return n + 1
				}
			}
		}
		return 1
	}
	return 1
}

// explainSinglePassParent reports whether a full scan under this parent runs
// once per process: a hash build (Hash, Parallel Hash), either side of a hash
// join, a Sort, or a Gather/Gather Merge. Nested Loop, Materialize, Memoize,
// and every other parent can rescan it per outer row.
func explainSinglePassParent(name string) bool {
	base := strings.TrimPrefix(name, "Parallel ")
	switch {
	case base == "Hash", name == "Sort", name == "Gather", name == "Gather Merge":
		return true
	default:
		return strings.HasPrefix(base, "Hash ") && strings.HasSuffix(base, " Join")
	}
}

// checkSummaryGenerationScans is the activeWorkSummaryQuery half of the
// #4446 guard, bound to the cost class #4446 and #6794 guard against (a
// per-row or per-group rescan of scope_generations), not to the scan type
// (#7009 arbiter ruling D1). For every "Seq Scan on scope_generations" line
// (Parallel Seq Scan included):
//
//   - R1: the nearest shallower plan line must be Hash, Parallel Hash, a hash
//     join (either side), Sort, Gather, or Gather Merge;
//   - R2: when actuals are present, loops on the scan and on that parent must
//     be at most Workers Launched + 1 of the nearest enclosing Gather, or 1;
//   - R3: at most one such scan inside active_fact_work_items, at most one
//     inside fact_work_history_counts, and none anywhere else (which keeps
//     the #6794 LATERAL LIMIT 1 scope-state probe); a plan with no
//     active_fact_work_items CTE fails closed;
//   - R4: every violation is reported, joined with errors.Join.
func checkSummaryGenerationScans(plan string) error {
	const (
		fullScan = "Seq Scan on scope_generations"
		detail   = "active_fact_work_items"
		history  = "fact_work_history_counts"
		outside  = "outside active_fact_work_items and fact_work_history_counts"
	)
	lines := strings.Split(plan, "\n")
	detailLo, detailHi, ok := explainCTERange(lines, detail)
	if !ok {
		return fmt.Errorf("plan has no %s CTE", detail)
	}
	historyLo, historyHi, _ := explainCTERange(lines, history)
	var errs []error
	counts := map[string]int{}
	for i, line := range lines {
		if !strings.Contains(line, fullScan) {
			continue
		}
		where := outside
		switch {
		case i > detailLo && i < detailHi:
			where = detail
		case i > historyLo && i < historyHi:
			where = history
		}
		counts[where]++
		parent, parentName := explainParent(lines, i), "plan root"
		if parent >= 0 {
			parentName = explainNodeName(lines[parent])
		}
		if !explainSinglePassParent(parentName) {
			errs = append(errs, fmt.Errorf("%s: full scope_generations scan under %q, want Hash, Parallel Hash, a hash join, Sort, Gather, or Gather Merge: %s",
				where, parentName, strings.TrimSpace(line)))
		}
		limit := explainLoopLimit(lines, i)
		if loops, ok := explainLoops(line); ok && loops > limit {
			errs = append(errs, fmt.Errorf("%s: full scope_generations scan loops=%d, want at most %d (Workers Launched + 1)", where, loops, limit))
		}
		if parent >= 0 {
			if loops, ok := explainLoops(lines[parent]); ok && loops > limit {
				errs = append(errs, fmt.Errorf("%s: full scope_generations scan parent %q loops=%d, want at most %d (Workers Launched + 1)", where, parentName, loops, limit))
			}
		}
	}
	for _, where := range []string{detail, history} {
		if counts[where] > 1 {
			errs = append(errs, fmt.Errorf("%s: %d full scope_generations scans, want at most 1", where, counts[where]))
		}
	}
	if counts[outside] > 0 {
		errs = append(errs, fmt.Errorf("%d full scope_generations scans %s", counts[outside], outside))
	}
	return errors.Join(errs...)
}
