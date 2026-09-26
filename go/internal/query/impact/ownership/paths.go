// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PathFilter is the result of FilterPaths: which paths survive and whether
// the ownership budget left some keys unchecked.
type PathFilter struct {
	// Keep[i] reports whether paths[i] is admitted whole.
	Keep []bool
	// Capped reports that CheckedKeyCap left keys unchecked; the caller must
	// report its result as truncated.
	Capped bool
}

// Kept returns how many paths survive.
func (f PathFilter) Kept() int {
	kept := 0
	for _, keep := range f.Keep {
		if keep {
			kept++
		}
	}
	return kept
}

// FilterPaths judges every node of every path on one bounded page with a
// single Check, then keeps a path only when every node on it is admitted. A
// path with no nodes is withheld: an empty or undecodable nodes(path) cannot
// be proven owned. Withheld paths are counted on
// eshu_dp_query_impact_scoped_paths_withheld_total by reason.
func (c Checker) FilterPaths(ctx context.Context, access querycontract.RepositoryAccessFilter, paths [][]Node) (PathFilter, error) {
	result := PathFilter{Keep: make([]bool, len(paths))}
	if !access.Scoped() {
		for i := range result.Keep {
			result.Keep[i] = true
		}
		return result, nil
	}
	var all []Node
	for _, path := range paths {
		all = append(all, path...)
	}
	verdict, err := c.Check(ctx, access, all)
	if err != nil {
		return PathFilter{}, err
	}
	result.Capped = verdict.Capped()
	withheld := map[string]int{}
	for i, path := range paths {
		if len(path) > 0 && verdict.AdmitsAll(path) {
			result.Keep[i] = true
			continue
		}
		withheld[c.withholdReason(verdict, path)]++
	}
	for reason, n := range withheld {
		RecordWithheld(ctx, c.Instruments, c.Route, reason, n)
	}
	return result, nil
}

// withholdReason names why a path was withheld: on the exposure route, a
// sink whose class no grant can own (WithheldSinkLabels); otherwise a key the
// budget left unchecked, or a node the grant does not own. The sink class
// wins because such a path is withheld whatever the budget decided.
func (c Checker) withholdReason(v Verdict, path []Node) string {
	if c.Route == RouteTraceExposurePath && len(path) > 0 && hasWithheldSinkLabel(path[len(path)-1].Labels) {
		return ReasonWithheldSinkClass
	}
	for _, node := range path {
		if v.Admits(node) {
			continue
		}
		class := ClassOf(node.Labels)
		if _, ok := v.unchecked[class][statementKey(class, node)]; ok {
			return ReasonUncheckedOverCap
		}
	}
	return ReasonUngrantedNode
}

// hasWithheldSinkLabel reports whether labels hold one of WithheldSinkLabels.
func hasWithheldSinkLabel(labels []string) bool {
	for _, label := range labels {
		for _, withheld := range WithheldSinkLabels {
			if label == withheld {
				return true
			}
		}
	}
	return false
}
