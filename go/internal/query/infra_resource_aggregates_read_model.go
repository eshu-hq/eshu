// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
	// the Postgres read model, and the graph-only labels (inventory.GraphOnlyLabels)
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
	if len(labels) == 0 || factsOnlyLabels(labels) {
		return true, nil
	}
	ready, err := s.ReadModel.Ready(ctx)
	if err != nil {
		return false, fmt.Errorf("check infra read model readiness: %w", err)
	}
	return ready, nil
}

// readModelSource names the store that serves a read-model read: the table
// alone when no mixed-writer graph branch is needed (every category that
// resolves without TerraformModule/TerraformOutput), else hybrid.
func readModelSource(graphMixed []string) InfraResourceAggregateSource {
	if len(graphMixed) == 0 {
		return InfraResourceAggregateSourceReadModel
	}
	return InfraResourceAggregateSourceHybrid
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

// infraReadModelGraphCypher renders the graph side of a read-model read as one
// CALL { ... UNION ALL ... } pass over the mixed-writer labels, each only
// through the indexed evidence_source seek (predicate first, so it anchors
// the branch), each with the request's filter clauses. Since #6843 no
// whole-label branch remains: the former graph-only labels come from fact
// truth in the Reader. Labels are allowlisted; values are bound parameters.
func infraReadModelGraphCypher(
	mixed []string,
	filter InfraResourceAggregateFilter,
	innerReturn func(label string) string,
	outerReturn string,
) string {
	branchWhere := infraResourceAggregateBranchWhere(filter)
	branches := make([]string, 0, len(mixed))
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

// infraMixedWriterCountCypher is the count route's graph pass: one CALL {
// ... UNION ALL ... } over the mixed-writer labels (see
// infraReadModelGraphCypher) that groups each branch by a static label
// literal, the provider bucket, and the environment bucket, so one graph
// read yields the mixed-writer rows for the total and all three rollups.
// Grouping happens inside each branch for the reason documented on
// infraResourceAggregatePerLabelCypher; measured on NornicDB, the combined-key
// marginals equal the single-key group-bys with no null keys.
func infraMixedWriterCountCypher(mixed []string, filter InfraResourceAggregateFilter) string {
	provider := infraResourceProviderGroupExpression(filter)
	return infraReadModelGraphCypher(mixed, filter, func(label string) string {
		return "RETURN '" + label + "' AS label, " + provider + " AS provider_bucket, " +
			infraResourceEnvironmentGroupExpression + " AS environment_bucket, count(n) AS bucket_count"
	}, "RETURN label, provider_bucket, environment_bucket, bucket_count")
}

// readModelTableFailed reports whether the table leg genuinely failed while
// the graph backend is usable: a non-cancellation table error with a healthy
// graph leg, or one the table failure itself canceled. A canceled table leg
// beside a genuinely failed graph leg means the graph broke first, so there
// is no fallback: the graph error (504/503 through WriteGraphReadError) must
// surface rather than a slow whole-label retry against a broken backend.
func readModelTableFailed(tableErr, graphErr error) bool {
	if tableErr == nil || errors.Is(tableErr, context.Canceled) {
		return false
	}
	return graphErr == nil || errors.Is(graphErr, context.Canceled)
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
	tableLabels, graphMixed := splitInfraLabels(labels)
	source := readModelSource(graphMixed)
	s.recordRead(ctx, "count", source)
	// Either leg failing cancels the other, so a table error does not wait
	// out the mixed-writer graph pass (and vice versa). The fallback below
	// runs on parentCtx: the derived ctx is already canceled once a leg fails.
	parentCtx := ctx
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
	if len(graphMixed) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			graphRows, graphErr = s.Graph.Run(ctx, infraMixedWriterCountCypher(graphMixed, filter),
				infraReadModelGraphParams(filter, graphMixed))
			if graphErr != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	// P2-2 fallback: the Postgres table leg failed while the graph is
	// usable. Serve the pre-read-model graph path instead of failing the
	// route; it reports source graph so the truth envelope stays honest.
	if readModelTableFailed(tableErr, graphErr) {
		return s.countFromGraph(parentCtx, labels, filter)
	}
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
	tableLabels, graphMixed := splitInfraLabels(labels)
	source := readModelSource(graphMixed)
	s.recordRead(ctx, "inventory", source)
	// See countFromReadModel: the fallback runs on the parent context.
	parentCtx := ctx
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
	if len(graphMixed) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cypher := infraReadModelGraphCypher(graphMixed, filter, func(string) string {
				return "RETURN " + groupExpr + " AS bucket, count(n) AS bucket_count"
			}, "RETURN bucket, bucket_count")
			graphRows, graphErr = s.Graph.Run(ctx, cypher, infraReadModelGraphParams(filter, graphMixed))
			if graphErr != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	// P2-2 fallback, mirroring the count route above.
	if readModelTableFailed(tableErr, graphErr) {
		return s.inventoryFromGraph(parentCtx, labels, filter, dimension, groupExpr, limit, offset)
	}
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

// countFromGraph serves the count route from the authoritative graph alone:
// one per-label total plus the provider, environment, and label rollups. It
// is the pre-read-model path for scoped and pre-ready callers, and the P2-2
// fallback when the read-model table leg fails: a Postgres outage must not
// fail an answer the graph holds whole. The fallback reports source graph,
// so the truth envelope names the store that actually served.
func (s GraphInfraResourceAggregateStore) countFromGraph(
	ctx context.Context,
	labels []string,
	filter InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	s.recordRead(ctx, "count", InfraResourceAggregateSourceGraph)

	branchWhere := infraResourceAggregateBranchWhere(filter)
	params := infraResourceAggregateParams(filter)

	// Per-label count branches each return exactly one count row (0 for an
	// empty label), so the total is their sum. Aggregating in Go avoids the
	// NornicDB CALL-subquery aggregation collapse documented in
	// infraResourceAggregatePerLabelCypher.
	//
	// Invariant: summing per-label counts equals the old whole-graph
	// `MATCH (n) WHERE (n:A OR n:B ...)` deduped total ONLY because every infra
	// resource node carries exactly one of the candidate labels — the
	// source-local projector materializes each cloud/IaC resource under a single
	// canonical label (CloudResource XOR TerraformResource XOR K8sResource XOR
	// ...), never a combination from allInfraLabels. A node carrying two
	// candidate labels would be counted once per label here and once total by
	// the old shape; if the projector ever assigns multiple infra labels to one
	// node, this count (and the buckets below) must switch to distinct-node
	// identity aggregation. The candidate taxonomy is `allInfraLabels`
	// (infra.go); TestInfraLabelsAreSinglePrimaryTaxonomy records this
	// single-label invariant so a taxonomy change surfaces the assumption.
	totalRows, err := s.Graph.Run(ctx,
		infraResourceAggregatePerLabelCypher(labels, branchWhere, "RETURN count(n) AS bucket_count", "RETURN bucket_count"),
		params)
	if err != nil {
		return InfraResourceAggregateCount{}, fmt.Errorf("count infra resources: %w", err)
	}
	var total int
	for _, row := range totalRows {
		total += IntVal(row, "bucket_count")
	}

	out := InfraResourceAggregateCount{
		TotalResources: total,
		ByProvider:     map[string]int{},
		ByEnvironment:  map[string]int{},
		ByLabel:        map[string]int{},
		Source:         InfraResourceAggregateSourceGraph,
	}
	if err := s.fillBuckets(ctx, labels, branchWhere, params,
		infraResourceProviderGroupExpression(filter),
		out.ByProvider); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, labels, branchWhere, params,
		"CASE WHEN n.environment IS NULL OR n.environment = '' THEN 'unknown' ELSE n.environment END",
		out.ByEnvironment); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	// Group by the node's primary label. `labels(n)` returns a list; we
	// surface the first label, which is the canonical type for these nodes.
	if err := s.fillBuckets(ctx, labels, branchWhere, params,
		"head(labels(n))",
		out.ByLabel); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	return out, nil
}

// inventoryFromGraph serves one inventory page from the authoritative graph
// alone. It is the pre-read-model path for scoped and pre-ready callers, and
// the P2-2 fallback when the read-model table leg fails, reporting source
// graph so the truth envelope names the serving store.
func (s GraphInfraResourceAggregateStore) inventoryFromGraph(
	ctx context.Context,
	labels []string,
	filter InfraResourceAggregateFilter,
	dimension InfraResourceInventoryDimension,
	groupExpr string,
	limit int,
	offset int,
) ([]InfraResourceInventoryRow, InfraResourceAggregateSource, error) {
	s.recordRead(ctx, "inventory", InfraResourceAggregateSourceGraph)

	branchWhere := infraResourceAggregateBranchWhere(filter)
	params := infraResourceAggregateParams(filter)

	// Fetch every per-label grouped bucket, then merge, order, and paginate in
	// Go. The distinct-bucket cardinality is small (bounded by the number of
	// distinct provider/environment/kind values across the fixed label set), so
	// fetching the full unioned set is cheap, and Go-side ordering/pagination
	// replaces the ORDER BY / SKIP / LIMIT that cannot run over a per-label CALL
	// subquery without triggering the NornicDB aggregation collapse documented
	// in infraResourceAggregatePerLabelCypher.
	cypher := infraResourceAggregatePerLabelCypher(labels, branchWhere,
		"RETURN "+groupExpr+" AS bucket, count(n) AS bucket_count",
		"RETURN bucket, bucket_count")
	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, "", fmt.Errorf("inventory infra resources: %w", err)
	}
	return paginateInfraResourceBuckets(mergeInfraResourceAggregateBuckets(rows), dimension, limit, offset),
		InfraResourceAggregateSourceGraph, nil
}
