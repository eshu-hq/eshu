// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// infraResourceAggregatePerLabelCypher builds a per-label
// CALL { ... UNION ALL ... } query over the fixed infrastructure label set.
//
// It replaces a single unlabeled `MATCH (n) WHERE (n:A OR n:B OR ...)` scan
// (the same whole-graph-scan defect fixed for infra/resources/search in
// #5278): the OR-of-labels predicate forces NornicDB to visit every node in
// the graph on every call before filtering, so a corpus of ~91k nodes pays a
// ~380-410ms whole-graph scan to aggregate ~5.6k infra nodes, and the cost
// grows with total graph size rather than the infra-label population.
// Anchoring one `MATCH (n:Label)` branch per candidate label keeps each scan
// bounded by that label's population (measured 380ms -> 90ms for the count,
// 410ms -> 1.2ms for a grouped bucket read on the same corpus).
//
// Two NornicDB v1.1.11 behaviours shape the exact structure and force
// application-side aggregation (see docs/public/reference/nornicdb-pitfalls.md):
//
//   - A bare top-level UNION chain returns zero rows for the entire query when
//     its first branch is empty; wrapping the union in CALL { ... } avoids it.
//     allInfraLabels starts with CloudResource, which is frequently empty.
//   - Outer aggregation over a CALL subquery result (e.g.
//     `CALL { ... } RETURN groupExpr AS bucket, count(n)`) evaluates the group
//     key to null and collapses every row into one bogus bucket. Measured
//     directly: the same read grouped inside each branch and passed through the
//     outer RETURN unchanged returns the correct per-branch rows. Callers
//     therefore group inside each branch and merge/sum the passed-through rows
//     in Go.
//
// branchWhere is the property/scope filter for one branch (already prefixed
// with " WHERE " when non-empty, "" otherwise); innerReturn is the branch's
// RETURN clause; outerReturn passes the unioned rows through without
// re-aggregating.
func infraResourceAggregatePerLabelCypher(labels []string, branchWhere, innerReturn, outerReturn string) string {
	branches := make([]string, 0, len(labels))
	for _, label := range labels {
		branches = append(branches, "MATCH (n:"+label+")"+branchWhere+" "+innerReturn)
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION ALL\n") + "\n}\n" + outerReturn
}

// infraResourceAggregateBranchWhere renders the property and scope filter
// predicates shared by every per-label branch, without the label predicate
// (each branch's `MATCH (n:Label)` supplies the label). It returns "" when no
// filters apply, otherwise " WHERE <clauses>".
func infraResourceAggregateBranchWhere(filter InfraResourceAggregateFilter) string {
	clauses := infraResourceAggregateFilterClauses(filter)
	if len(clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(clauses, " AND ")
}

// infraResourceAggregateBucketRow is one (bucket, count) pair passed through
// the outer RETURN of a per-label aggregate query before Go-side merge.
type infraResourceAggregateBucketRow struct {
	Bucket string
	Count  int
}

// mergeInfraResourceAggregateBuckets sums the per-branch (bucket, count) rows
// into a single bucket total map. A bucket value (for example a provider or
// environment) can appear once per contributing label, so the per-branch rows
// must be summed, not overwritten.
//
// Zero-count rows are skipped: a per-label branch whose label is empty (or
// filtered to nothing) still emits one grouped row with a null bucket and
// count 0, because `RETURN groupExpr, count(n)` over no matches aggregates to a
// single row. The old whole-graph `MATCH (n) WHERE (n:A OR ...)` read grouped
// only over matched nodes and never produced an empty bucket, so dropping the
// zero-count rows keeps the result exactly equivalent (a real bucket always has
// count >= 1, since count(n) groups only over nodes that exist).
func mergeInfraResourceAggregateBuckets(rows []map[string]any) map[string]int {
	merged := map[string]int{}
	for _, row := range rows {
		count := IntVal(row, "bucket_count")
		if count <= 0 {
			continue
		}
		bucket := strings.TrimSpace(StringVal(row, "bucket"))
		merged[bucket] += count
	}
	return merged
}

// sortedInfraResourceAggregateBuckets orders merged buckets the way the graph
// query previously ordered them: descending count, then bucket name ascending
// as a deterministic tie-break. This ordering now happens in Go because the
// per-label CALL rows are aggregated application-side.
func sortedInfraResourceAggregateBuckets(merged map[string]int) []infraResourceAggregateBucketRow {
	rows := make([]infraResourceAggregateBucketRow, 0, len(merged))
	for bucket, count := range merged {
		rows = append(rows, infraResourceAggregateBucketRow{Bucket: bucket, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Bucket < rows[j].Bucket
	})
	return rows
}
