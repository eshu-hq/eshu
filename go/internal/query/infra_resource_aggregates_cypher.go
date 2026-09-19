// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// InfraResourceAggregateSource names the store that served an aggregate read.
type InfraResourceAggregateSource string

const (
	// InfraResourceAggregateSourceGraph means every label was read from the
	// graph: a scoped caller, or a read before the read model's backfill
	// marker exists.
	InfraResourceAggregateSourceGraph InfraResourceAggregateSource = "graph"
	// InfraResourceAggregateSourceReadModel means every resolved label came
	// from the Postgres read model alone (for example category=k8s).
	InfraResourceAggregateSourceReadModel InfraResourceAggregateSource = "read_model"
	// InfraResourceAggregateSourceHybrid means content-derived nodes came from
	// the Postgres read model, and the graph-only labels (infraGraphOnlyLabels)
	// plus the mixed-writer labels' other nodes (infraMixedWriterGraphSource)
	// from one graph pass in the same read.
	InfraResourceAggregateSourceHybrid InfraResourceAggregateSource = "hybrid"
)

// InfraResourceReadModel is the Postgres infra read model
// (storage/postgres/infra/inventory.Reader). Its counts equal the graph's for
// every label in inventory.Labels: the canonical node writer is the only
// writer of those labels and copies the same trimmed metadata from the same
// content rows.
type InfraResourceReadModel interface {
	Ready(context.Context) (bool, error)
	CountBuckets(context.Context, inventory.Filter) ([]inventory.CountBucket, error)
	DimensionBuckets(context.Context, inventory.Filter, inventory.Dimension) (map[string]int64, error)
}

// WithReadModel returns a copy that serves unscoped reads from readModel once
// it reports Ready: its backfill marker exists and no repository waits for a
// repair after a write from a binary that does not derive.
func (s GraphInfraResourceAggregateStore) WithReadModel(readModel InfraResourceReadModel) GraphInfraResourceAggregateStore {
	s.ReadModel = readModel
	return s
}

// WithInstruments returns a copy that records eshu_dp_infra_inventory_reads_total.
func (s GraphInfraResourceAggregateStore) WithInstruments(instruments *telemetry.Instruments) GraphInfraResourceAggregateStore {
	s.Instruments = instruments
	return s
}

// readModelServes reports whether the read-model path serves filter over
// labels. Scoped callers always stay on the graph: two infra labels are
// authorized through USES and MATCHES_STATE edges a repo-keyed table cannot
// express. When labels holds no read-model label (category=cloud), the path
// reads only the graph, so it serves without checking readiness: a Postgres
// failure must not fail an answer the graph holds whole. Otherwise a failed
// marker check fails the read rather than silently switching stores.
func (s GraphInfraResourceAggregateStore) readModelServes(
	ctx context.Context,
	filter InfraResourceAggregateFilter,
	labels []string,
) (bool, error) {
	if s.ReadModel == nil || filter.scoped() {
		return false, nil
	}
	if table, _, _ := splitInfraLabels(labels); len(table) == 0 {
		return true, nil
	}
	ready, err := s.ReadModel.Ready(ctx)
	if err != nil {
		return false, fmt.Errorf("check infra read model readiness: %w", err)
	}
	return ready, nil
}

// readModelSource names the store that serves a read-model read of this label
// split: the graph alone when no read-model label resolved (category=cloud),
// the table alone when no graph branch is needed (category=k8s), else hybrid.
func readModelSource(table, graphWhole, graphMixed []string) InfraResourceAggregateSource {
	switch {
	case len(table) == 0:
		return InfraResourceAggregateSourceGraph
	case len(graphWhole)+len(graphMixed) == 0:
		return InfraResourceAggregateSourceReadModel
	default:
		return InfraResourceAggregateSourceHybrid
	}
}

// recordRead counts one read: source "read_model" whenever the table served
// any of it (read_model or hybrid), else "graph".
func (s GraphInfraResourceAggregateStore) recordRead(ctx context.Context, route string, source InfraResourceAggregateSource) {
	if source == InfraResourceAggregateSourceHybrid {
		source = InfraResourceAggregateSourceReadModel
	}
	if s.Instruments == nil || s.Instruments.InfraInventoryReads == nil {
		return
	}
	s.Instruments.InfraInventoryReads.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrRoute(route), telemetry.AttrSource(string(source))))
}

// splitInfraLabels partitions the resolved label set, keeping input order:
// table is every read-model label (inventory.Labels), graphWhole the
// graph-only labels read whole, and graphMixed the read-model labels whose
// other writer's nodes the graph adds (infraMixedWriterGraphSource).
func splitInfraLabels(labels []string) (table []string, graphWhole []string, graphMixed []string) {
	graphOnly := make(map[string]struct{}, len(infraGraphOnlyLabels))
	for _, label := range infraGraphOnlyLabels {
		graphOnly[label] = struct{}{}
	}
	for _, label := range labels {
		if _, ok := graphOnly[label]; ok {
			graphWhole = append(graphWhole, label)
			continue
		}
		table = append(table, label)
		if _, ok := infraMixedWriterGraphSource[label]; ok {
			graphMixed = append(graphMixed, label)
		}
	}
	return table, graphWhole, graphMixed
}

// infraReadModelGraphCypher renders the graph side of a read-model read as one
// CALL { ... UNION ALL ... } pass: each graph-only label whole, and each
// mixed-writer label only through the indexed evidence_source seek (predicate
// first, so it anchors the branch), each with the request's filter clauses.
// Labels are allowlisted; values are bound parameters.
func infraReadModelGraphCypher(
	whole []string,
	mixed []string,
	filter InfraResourceAggregateFilter,
	innerReturn func(label string) string,
	outerReturn string,
) string {
	branchWhere := infraResourceAggregateBranchWhere(filter)
	branches := make([]string, 0, len(whole)+len(mixed))
	for _, label := range whole {
		if querycontract.InfraLabelAllowed(label) {
			branches = append(branches, "MATCH (n:"+label+")"+branchWhere+" "+innerReturn(label))
		}
	}
	for _, label := range mixed {
		if !querycontract.InfraLabelAllowed(label) {
			continue
		}
		where := " WHERE n.evidence_source = $graph_writer_evidence_source"
		if branchWhere != "" {
			where += " AND " + strings.TrimPrefix(branchWhere, " WHERE ")
		}
		branches = append(branches, "MATCH (n:"+label+")"+where+" "+innerReturn(label))
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION ALL\n") + "\n}\n" + outerReturn
}

// infraReadModelGraphParams binds the filter parameters, plus the mixed-writer
// evidence_source when a mixed branch is present.
func infraReadModelGraphParams(filter InfraResourceAggregateFilter, mixed []string) map[string]any {
	params := infraResourceAggregateParams(filter)
	if len(mixed) > 0 {
		params["graph_writer_evidence_source"] = infraMixedWriterEvidenceSource
	}
	return params
}

func infraReadModelFilter(filter InfraResourceAggregateFilter, labels []string) inventory.Filter {
	return inventory.Filter{
		Labels:           labels,
		AllCategories:    infraResourceAggregateAllCategories(filter),
		Kind:             filter.Kind,
		ResourceType:     filter.ResourceType,
		Provider:         filter.Provider,
		Environment:      filter.Environment,
		ResourceService:  filter.ResourceService,
		ResourceCategory: filter.ResourceCategory,
	}
}

// infraResourceEnvironmentGroupExpression is the graph environment bucket,
// shared by every aggregate read.
const infraResourceEnvironmentGroupExpression = "CASE WHEN n.environment IS NULL OR n.environment = '' THEN 'unknown' ELSE n.environment END"

// infraGraphOnlyCountCypher is the count route's graph pass: one CALL {
// ... UNION ALL ... } over the graph labels (see infraReadModelGraphCypher)
// that groups each branch by a static label literal, the provider bucket, and
// the environment bucket, so one graph read yields the total and all three
// rollups. Grouping happens inside each branch for the reason documented on
// infraResourceAggregatePerLabelCypher; measured on NornicDB, the combined-key
// marginals equal the single-key group-bys with no null keys.
func infraGraphOnlyCountCypher(whole []string, mixed []string, filter InfraResourceAggregateFilter) string {
	provider := infraResourceProviderGroupExpression(filter)
	return infraReadModelGraphCypher(whole, mixed, filter, func(label string) string {
		return "RETURN '" + label + "' AS label, " + provider + " AS provider_bucket, " +
			infraResourceEnvironmentGroupExpression + " AS environment_bucket, count(n) AS bucket_count"
	}, "RETURN label, provider_bucket, environment_bucket, bucket_count")
}

// countFromReadModel serves the count route from the read model for the
// content-derived nodes and one combined graph pass for the rest,
// run concurrently. total = sum(by_label) holds because every infra node
// carries exactly one candidate label (see CountInfraResources).
func (s GraphInfraResourceAggregateStore) countFromReadModel(
	ctx context.Context,
	labels []string,
	filter InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	tableLabels, graphWhole, graphMixed := splitInfraLabels(labels)
	source := readModelSource(tableLabels, graphWhole, graphMixed)
	s.recordRead(ctx, "count", source)
	// Either leg failing cancels the other, so a table error does not wait
	// out a graph label scan (and vice versa).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg        sync.WaitGroup
		buckets   []inventory.CountBucket
		graphRows []map[string]any
		tableErr  error
		graphErr  error
	)
	if len(tableLabels) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buckets, tableErr = s.ReadModel.CountBuckets(ctx, infraReadModelFilter(filter, tableLabels))
			if tableErr != nil {
				cancel()
			}
		}()
	}
	if len(graphWhole)+len(graphMixed) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			graphRows, graphErr = s.Graph.Run(ctx, infraGraphOnlyCountCypher(graphWhole, graphMixed, filter),
				infraReadModelGraphParams(filter, graphMixed))
			if graphErr != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	if err := readModelLegError(tableErr, graphErr,
		"count infra resources from read model", "count graph infra resources"); err != nil {
		return InfraResourceAggregateCount{}, err
	}

	out := InfraResourceAggregateCount{
		ByProvider:    map[string]int{},
		ByEnvironment: map[string]int{},
		ByLabel:       map[string]int{},
		Source:        source,
	}
	add := func(label, provider, environment string, count int) {
		if count <= 0 {
			return
		}
		out.TotalResources += count
		out.ByLabel[label] += count
		out.ByProvider[strings.TrimSpace(provider)] += count
		out.ByEnvironment[strings.TrimSpace(environment)] += count
	}
	for _, bucket := range buckets {
		add(bucket.Label, bucket.Provider, bucket.Environment, int(bucket.Count))
	}
	// A graph branch that matches nothing yields one zero-count row with
	// null keys; add skips it, matching the whole-graph read that never
	// produced an empty bucket.
	for _, row := range graphRows {
		add(StringVal(row, "label"), StringVal(row, "provider_bucket"),
			StringVal(row, "environment_bucket"), IntVal(row, "bucket_count"))
	}
	return out, nil
}

// inventoryFromReadModel serves one inventory page from the read model for the
// content-derived nodes and one grouped graph pass for the rest,
// run concurrently, then merges, orders, and paginates exactly as the graph
// path does.
func (s GraphInfraResourceAggregateStore) inventoryFromReadModel(
	ctx context.Context,
	labels []string,
	filter InfraResourceAggregateFilter,
	dimension InfraResourceInventoryDimension,
	groupExpr string,
	limit int,
	offset int,
) ([]InfraResourceInventoryRow, InfraResourceAggregateSource, error) {
	tableLabels, graphWhole, graphMixed := splitInfraLabels(labels)
	source := readModelSource(tableLabels, graphWhole, graphMixed)
	s.recordRead(ctx, "inventory", source)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg        sync.WaitGroup
		tableRows map[string]int64
		graphRows []map[string]any
		tableErr  error
		graphErr  error
	)
	if len(tableLabels) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tableRows, tableErr = s.ReadModel.DimensionBuckets(ctx, infraReadModelFilter(filter, tableLabels),
				inventory.Dimension(dimension))
			if tableErr != nil {
				cancel()
			}
		}()
	}
	if len(graphWhole)+len(graphMixed) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cypher := infraReadModelGraphCypher(graphWhole, graphMixed, filter, func(string) string {
				return "RETURN " + groupExpr + " AS bucket, count(n) AS bucket_count"
			}, "RETURN bucket, bucket_count")
			graphRows, graphErr = s.Graph.Run(ctx, cypher, infraReadModelGraphParams(filter, graphMixed))
			if graphErr != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	if err := readModelLegError(tableErr, graphErr,
		"inventory infra resources from read model", "inventory graph infra resources"); err != nil {
		return nil, "", err
	}
	merged := mergeInfraResourceAggregateBuckets(graphRows)
	for bucket, count := range tableRows {
		if count > 0 {
			merged[strings.TrimSpace(bucket)] += int(count)
		}
	}
	return paginateInfraResourceBuckets(merged, dimension, limit, offset), source, nil
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
