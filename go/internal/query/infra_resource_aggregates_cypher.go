// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
	// InfraResourceAggregateSourceReadModel means the entity-derived labels
	// came from the Postgres read model and the graph-only labels
	// (infraGraphOnlyLabels) from one graph pass.
	InfraResourceAggregateSourceReadModel InfraResourceAggregateSource = "read_model"
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
// its backfill marker exists.
func (s GraphInfraResourceAggregateStore) WithReadModel(readModel InfraResourceReadModel) GraphInfraResourceAggregateStore {
	s.ReadModel = readModel
	return s
}

// WithInstruments returns a copy that records eshu_dp_infra_inventory_reads_total.
func (s GraphInfraResourceAggregateStore) WithInstruments(instruments *telemetry.Instruments) GraphInfraResourceAggregateStore {
	s.Instruments = instruments
	return s
}

// readModelServes reports whether the read model serves filter. Scoped
// callers always stay on the graph: two infra labels are authorized through
// USES and MATCHES_STATE edges a repo-keyed table cannot express. A failed
// marker check fails the read rather than silently switching stores.
func (s GraphInfraResourceAggregateStore) readModelServes(ctx context.Context, filter InfraResourceAggregateFilter) (bool, error) {
	if s.ReadModel == nil || filter.scoped() {
		return false, nil
	}
	ready, err := s.ReadModel.Ready(ctx)
	if err != nil {
		return false, fmt.Errorf("check infra read model backfill marker: %w", err)
	}
	return ready, nil
}

func (s GraphInfraResourceAggregateStore) recordRead(ctx context.Context, route string, source InfraResourceAggregateSource) {
	if s.Instruments == nil || s.Instruments.InfraInventoryReads == nil {
		return
	}
	s.Instruments.InfraInventoryReads.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrRoute(route), telemetry.AttrSource(string(source))))
}

// splitInfraLabels partitions the resolved label set into read-model labels
// and graph-only labels, keeping the input order.
func splitInfraLabels(labels []string) (table []string, graph []string) {
	graphOnly := make(map[string]struct{}, len(infraGraphOnlyLabels))
	for _, label := range infraGraphOnlyLabels {
		graphOnly[label] = struct{}{}
	}
	for _, label := range labels {
		if _, ok := graphOnly[label]; ok {
			graph = append(graph, label)
		} else {
			table = append(table, label)
		}
	}
	return table, graph
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

// infraGraphOnlyCountCypher is one CALL { ... UNION ALL ... } pass over the
// graph-only labels that groups each branch by a static label literal, the
// provider bucket, and the environment bucket, so the count route gets its
// total and all three rollups from one graph read instead of four. Grouping
// happens inside each branch for the reason documented on
// infraResourceAggregatePerLabelCypher. Measured on NornicDB at production
// scale: the combined-key marginals equal the single-key group-bys for all
// four graph-only labels, with no null keys.
func infraGraphOnlyCountCypher(labels []string, filter InfraResourceAggregateFilter) string {
	branchWhere := infraResourceAggregateBranchWhere(filter)
	provider := infraResourceProviderGroupExpression(filter)
	branches := make([]string, 0, len(labels))
	for _, label := range labels {
		if !querycontract.InfraLabelAllowed(label) {
			continue
		}
		branches = append(branches, "MATCH (n:"+label+")"+branchWhere+
			" RETURN '"+label+"' AS label, "+provider+" AS provider_bucket, "+
			infraResourceEnvironmentGroupExpression+" AS environment_bucket, count(n) AS bucket_count")
	}
	return "CALL {\n" + strings.Join(branches, "\nUNION ALL\n") +
		"\n}\nRETURN label, provider_bucket, environment_bucket, bucket_count"
}

// countFromReadModel serves the count route from the read model for the
// entity-derived labels and one combined graph pass for the graph-only labels,
// run concurrently. total = sum(by_label) holds because every infra node
// carries exactly one candidate label (see CountInfraResources).
func (s GraphInfraResourceAggregateStore) countFromReadModel(
	ctx context.Context,
	labels []string,
	filter InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	tableLabels, graphLabels := splitInfraLabels(labels)
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
		}()
	}
	if len(graphLabels) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			graphRows, graphErr = s.Graph.Run(ctx, infraGraphOnlyCountCypher(graphLabels, filter),
				infraResourceAggregateParams(filter))
		}()
	}
	wg.Wait()
	if tableErr != nil {
		return InfraResourceAggregateCount{}, fmt.Errorf("count infra resources from read model: %w", tableErr)
	}
	if graphErr != nil {
		return InfraResourceAggregateCount{}, fmt.Errorf("count graph-only infra resources: %w", graphErr)
	}

	out := InfraResourceAggregateCount{
		ByProvider:    map[string]int{},
		ByEnvironment: map[string]int{},
		ByLabel:       map[string]int{},
		Source:        InfraResourceAggregateSourceReadModel,
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
	// A graph-only branch over an empty label yields one zero-count row with
	// null keys; add skips it, matching the whole-graph read that never
	// produced an empty bucket.
	for _, row := range graphRows {
		add(StringVal(row, "label"), StringVal(row, "provider_bucket"),
			StringVal(row, "environment_bucket"), IntVal(row, "bucket_count"))
	}
	return out, nil
}

// inventoryFromReadModel serves one inventory page from the read model for the
// entity-derived labels and one grouped graph pass for the graph-only labels,
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
) ([]InfraResourceInventoryRow, error) {
	tableLabels, graphLabels := splitInfraLabels(labels)
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
		}()
	}
	if len(graphLabels) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cypher := infraResourceAggregatePerLabelCypher(graphLabels, infraResourceAggregateBranchWhere(filter),
				"RETURN "+groupExpr+" AS bucket, count(n) AS bucket_count",
				"RETURN bucket, bucket_count")
			graphRows, graphErr = s.Graph.Run(ctx, cypher, infraResourceAggregateParams(filter))
		}()
	}
	wg.Wait()
	if tableErr != nil {
		return nil, fmt.Errorf("inventory infra resources from read model: %w", tableErr)
	}
	if graphErr != nil {
		return nil, fmt.Errorf("inventory graph-only infra resources: %w", graphErr)
	}
	merged := mergeInfraResourceAggregateBuckets(graphRows)
	for bucket, count := range tableRows {
		if count > 0 {
			merged[strings.TrimSpace(bucket)] += int(count)
		}
	}
	return paginateInfraResourceBuckets(merged, dimension, limit, offset), nil
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

// StartInfraInventoryBackfill runs the infra read model backfill in the
// background until it records its marker or ctx ends. Readers stay on the
// graph until the marker exists, so startup never waits on it. Running it from
// more than one process at once is safe: the processes serialize per
// repository on the derive lock and the marker insert is idempotent. A failure
// is logged and retried on the next process start.
func StartInfraInventoryBackfill(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	if db == nil {
		return
	}
	go func() {
		backfiller := inventory.Backfiller{DB: postgres.SQLDB{DB: db}, Logger: logger}
		if _, err := backfiller.Run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.ErrorContext(ctx, "infra inventory backfill failed; aggregate reads stay on the graph until a later start completes it",
				"event_name", "infra_inventory.backfill.failed", "error", err)
		}
	}()
}

// RunStartupBackfills runs the query surface's startup backfills for a process
// that mounts graph-backed routes: the blocking CloudResource owner-ledger
// seed (#5563), then the background infra read model backfill (#6793). An
// owner-ledger failure aborts startup exactly as before. The infra backfill
// never blocks startup.
func RunStartupBackfills(ctx context.Context, db *sql.DB, graph GraphQuery, logger *slog.Logger) error {
	if err := BackfillCloudResourceOwnerLedger(ctx, db, graph); err != nil {
		return err
	}
	StartInfraInventoryBackfill(ctx, db, logger)
	return nil
}
