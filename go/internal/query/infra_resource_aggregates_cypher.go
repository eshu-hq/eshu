// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// paginateInfraResourceBuckets orders merged buckets and cuts one page.
func paginateInfraResourceBuckets(
	merged map[string]int,
	dimension InfraResourceInventoryDimension,
	limit int,
	offset int,
) []InfraResourceInventoryRow {
	sorted := sortedInfraResourceAggregateBuckets(merged)
	out := make([]InfraResourceInventoryRow, 0, limit)
	for i := offset; i < len(sorted) && len(out) < limit; i++ {
		out = append(out, InfraResourceInventoryRow{
			Dimension: dimension,
			Value:     sorted[i].Bucket,
			Count:     sorted[i].Count,
		})
	}
	return out
}

// splitInfraLabels partitions the resolved label set: table is every label
// the Reader serves (inventory.Labels from the entities table plus the
// fact-served graph-only labels, inventory.GraphOnlyLabels, partitioned
// inside the Reader), keeping input order, and graphMixed the read-model
// labels whose other writer's nodes one graph pass still adds
// (infraMixedWriterGraphSource). Since #6843 no label needs a whole-label
// graph pass.
func splitInfraLabels(labels []string) (table []string, graphMixed []string) {
	table = append([]string(nil), labels...)
	for _, label := range labels {
		if _, ok := infraMixedWriterGraphSource[label]; ok {
			graphMixed = append(graphMixed, label)
		}
	}
	return table, graphMixed
}

// factsOnlyLabels reports whether every label is fact-served
// (inventory.GraphOnlyLabels). Fact truth needs no backfill marker: it is
// current from normal pipeline operation, so facts-only reads serve without
// checking readiness, preserving the pre-#6843 availability of the
// graph-only routes before the entities backfill completes.
func factsOnlyLabels(labels []string) bool {
	facts := make(map[string]struct{}, len(inventory.GraphOnlyLabels))
	for _, label := range inventory.GraphOnlyLabels {
		facts[label] = struct{}{}
	}
	for _, label := range labels {
		if _, ok := facts[label]; !ok {
			return false
		}
	}
	return true
}

// NewInfraResourceAggregateStore wires the aggregate store the API and MCP
// server mount: the graph for every read, plus the Postgres infra read model
// when db is non-nil. A nil db (graph-only wiring, tests) keeps every read on
// the graph.
func NewInfraResourceAggregateStore(graph GraphQuery, db *sql.DB, instruments *telemetry.Instruments) GraphInfraResourceAggregateStore {
	store := NewGraphInfraResourceAggregateStore(graph).WithInstruments(instruments)
	if db == nil {
		return store
	}
	return store.WithReadModel(inventory.Reader{DB: postgres.SQLDB{DB: db}})
}
