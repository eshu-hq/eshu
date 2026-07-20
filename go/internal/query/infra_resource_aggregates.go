// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

// InfraResourceAggregateStore reads cheap-summary aggregates over the
// graph-backed infrastructure resource corpus (CloudResource,
// TerraformResource, K8sResource, CloudFormationResource, ArgoCDApplication,
// CrossplaneXRD, HelmChart, and friends; see `allInfraLabels` in infra.go). It replaces
// the page-and-iterate caller workflow for ecosystem-level questions like
// "how many resources per provider?" or "how many Terraform resources per
// account?" exposed by find_infra_resources.
//
// Performance contract: hot-path eligibility requires a label-property
// anchor. The aggregate anchors one `MATCH (n:Label)` branch per candidate
// label inside a CALL subquery (see infraResourceAggregatePerLabelCypher),
// replacing the earlier unlabeled `MATCH (n)` filtered by an OR of the
// documented infrastructure labels — that whole-graph scan cost grew with
// total graph size rather than the infra-label population (#5281, the same
// defect fixed for infra/resources/search in #5278). The `category` filter
// narrows the label set to one of {k8s, terraform, argocd, crossplane, helm,
// cloud}; when combined with an indexed property predicate that label exposes,
// each branch hits the cookbook Area-5 hot path.
type InfraResourceAggregateStore interface {
	CountInfraResources(context.Context, InfraResourceAggregateFilter) (InfraResourceAggregateCount, error)
	InfraResourceInventory(
		context.Context,
		InfraResourceAggregateFilter,
		InfraResourceInventoryDimension,
		int,
		int,
	) ([]InfraResourceInventoryRow, error)
}

// InfraResourceInventoryDimension names the grouping dimension for the
// inventory aggregate.
type InfraResourceInventoryDimension string

const (
	// InfraResourceInventoryByProvider groups by `n.provider` (cloud
	// provider: aws, gcp, azure, etc.).
	InfraResourceInventoryByProvider InfraResourceInventoryDimension = "provider"
	// InfraResourceInventoryByEnvironment groups by `n.environment`.
	InfraResourceInventoryByEnvironment InfraResourceInventoryDimension = "environment"
	// InfraResourceInventoryByResourceCategory groups by `n.resource_category`
	// (compute / storage / network / ...).
	InfraResourceInventoryByResourceCategory InfraResourceInventoryDimension = "resource_category"
	// InfraResourceInventoryByResourceService groups by `n.resource_service`
	// (aws.ec2, aws.s3, k8s.workload, ...).
	InfraResourceInventoryByResourceService InfraResourceInventoryDimension = "resource_service"
	// InfraResourceInventoryByLabel groups by the node's primary label
	// (TerraformResource / K8sResource / CloudFormationResource / ...).
	// Useful for "what kinds of infrastructure do we ingest?" prompts.
	InfraResourceInventoryByLabel InfraResourceInventoryDimension = "label"
)

// InfraResourceAggregateMaxLimit caps inventory result pages.
const InfraResourceAggregateMaxLimit = 500

// InfraResourceAggregateFilter narrows aggregate reads. `Category` selects
// one of the documented infraCategoryLabels keys (k8s, terraform, argocd,
// crossplane, helm); empty means all infrastructure labels.
//
// AllowedRepositoryIDs / AllowedScopeIDs carry a scoped-token's granted
// repository and ingestion-scope ids. When either is non-empty the aggregate
// binds a repository-anchored predicate so counts, rollups, inventory buckets,
// and totals are computed over only the resources attributable to the granted
// repositories (see infraResourceScopePredicate). Both empty means unrestricted
// (shared / admin / local), and the rendered Cypher is byte-identical to the
// pre-scoped query. The handler short-circuits empty-grant scoped tokens before
// the store is ever called, so a populated filter always has at least one id.
type InfraResourceAggregateFilter struct {
	Category             string
	Kind                 string
	ResourceType         string
	Provider             string
	Environment          string
	ResourceService      string
	ResourceCategory     string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// scoped reports whether the filter carries a scoped-token grant set that must
// bound the aggregate to repository-attributable resources.
func (f InfraResourceAggregateFilter) scoped() bool {
	return len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0
}

// InfraResourceAggregateCount is the cheap-summary totals envelope used by
// the count handler. ByProvider / ByEnvironment / ByLabel are pre-aggregated
// rollups so callers can answer the three most common per-dimension
// questions without a second round trip.
type InfraResourceAggregateCount struct {
	TotalResources int
	ByProvider     map[string]int
	ByEnvironment  map[string]int
	ByLabel        map[string]int
}

// InfraResourceInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type InfraResourceInventoryRow struct {
	Dimension InfraResourceInventoryDimension `json:"dimension"`
	Value     string                          `json:"value"`
	Count     int                             `json:"count"`
}

// GraphInfraResourceAggregateStore reads aggregate counts via the
// `GraphQuery` port.
type GraphInfraResourceAggregateStore struct {
	Graph GraphQuery
}

// NewGraphInfraResourceAggregateStore wires a GraphQuery (Neo4jReader in
// production) into the infra resource aggregate Reader.
func NewGraphInfraResourceAggregateStore(graph GraphQuery) GraphInfraResourceAggregateStore {
	return GraphInfraResourceAggregateStore{Graph: graph}
}

// resolveInfraLabels picks the label set the aggregate scans. An empty
// category means "all documented infra labels" (the same default the list
// endpoint uses); otherwise the category narrows to one label-set from
// `infraCategoryLabels` defined in infra.go.
func resolveInfraLabels(category string) ([]string, error) {
	if strings.TrimSpace(category) == "" {
		return allInfraLabels, nil
	}
	mapped, ok := infraCategoryLabels[strings.ToLower(category)]
	if !ok {
		return nil, fmt.Errorf("unsupported infra category: %q", category)
	}
	return mapped, nil
}

// CountInfraResources returns the cheap-summary totals envelope for the
// scoped infrastructure slice.
func (s GraphInfraResourceAggregateStore) CountInfraResources(
	ctx context.Context,
	filter InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	if s.Graph == nil {
		return InfraResourceAggregateCount{}, fmt.Errorf("infra resource aggregate graph is required")
	}
	labels, err := resolveInfraLabels(filter.Category)
	if err != nil {
		return InfraResourceAggregateCount{}, err
	}

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

func (s GraphInfraResourceAggregateStore) fillBuckets(
	ctx context.Context,
	labels []string,
	branchWhere string,
	params map[string]any,
	groupExpr string,
	dst map[string]int,
) error {
	cypher := infraResourceAggregatePerLabelCypher(labels, branchWhere,
		"RETURN "+groupExpr+" AS bucket, count(n) AS bucket_count",
		"RETURN bucket, bucket_count")
	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("group infra resources: %w", err)
	}
	// Each label branch groups independently, so one bucket value can appear
	// once per contributing label; sum the per-branch counts.
	for bucket, count := range mergeInfraResourceAggregateBuckets(rows) {
		dst[bucket] = count
	}
	return nil
}

// InfraResourceInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s GraphInfraResourceAggregateStore) InfraResourceInventory(
	ctx context.Context,
	filter InfraResourceAggregateFilter,
	dimension InfraResourceInventoryDimension,
	limit int,
	offset int,
) ([]InfraResourceInventoryRow, error) {
	if s.Graph == nil {
		return nil, fmt.Errorf("infra resource aggregate graph is required")
	}
	groupExpr, err := infraResourceInventoryGroupExpression(dimension, filter)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > InfraResourceAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", InfraResourceAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	labels, err := resolveInfraLabels(filter.Category)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("inventory infra resources: %w", err)
	}
	sorted := sortedInfraResourceAggregateBuckets(mergeInfraResourceAggregateBuckets(rows))
	out := make([]InfraResourceInventoryRow, 0, limit)
	for i := offset; i < len(sorted) && len(out) < limit; i++ {
		out = append(out, InfraResourceInventoryRow{
			Dimension: dimension,
			Value:     sorted[i].Bucket,
			Count:     sorted[i].Count,
		})
	}
	return out, nil
}

// infraResourceAggregateFilterClauses renders the optional indexed-property
// filters shared by every per-label aggregate branch, WITHOUT any label
// predicate (each branch's `MATCH (n:Label)` supplies the label). Filter
// values flow through bound parameters; nothing user-supplied is interpolated.
//
// Property predicates use direct equality on TerraformResource fields for
// category-specific Terraform reads. The clauses only render when the caller
// passed a non-empty filter value, so the coalesce-wrapped form is semantically
// equivalent to direct equality (Cypher equality is null-rejecting). Direct
// equality keeps the predicate eligible for the `tf_resource_provider` /
// `tf_resource_environment` / `tf_resource_service` / `tf_resource_category`
// indexes on TerraformResource; the coalesce wrapper would block planner
// index selection. The all-category scope uses an OR across equivalent
// provider/service fields so CloudResource rows remain reachable.
func infraResourceAggregateFilterClauses(filter InfraResourceAggregateFilter) []string {
	clauses := []string{}
	if filter.Kind != "" {
		if infraResourceAggregateCanReachCloud(filter) {
			clauses = append(clauses, "(n.kind = $kind OR n.resource_type = $kind OR n.data_type = $kind OR n.service_kind = $kind)")
		} else {
			clauses = append(clauses, "(n.kind = $kind OR n.resource_type = $kind OR n.data_type = $kind)")
		}
	}
	if filter.ResourceType != "" {
		clauses = append(clauses, "(n.resource_type = $resource_type OR n.data_type = $resource_type)")
	}
	if filter.Provider != "" {
		if infraResourceAggregateCloudCategory(filter) {
			clauses = append(clauses, "n.source_system = $provider")
		} else if infraResourceAggregateAllCategories(filter) {
			clauses = append(clauses, "(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))")
		} else {
			clauses = append(clauses, "n.provider = $provider")
		}
	}
	if filter.Environment != "" {
		clauses = append(clauses, "n.environment = $environment")
	}
	if filter.ResourceService != "" {
		if infraResourceAggregateCloudCategory(filter) {
			clauses = append(clauses, "n.service_kind = $resource_service")
		} else if infraResourceAggregateAllCategories(filter) {
			clauses = append(clauses, "(n.resource_service = $resource_service OR n.service_kind = $resource_service)")
		} else {
			clauses = append(clauses, "n.resource_service = $resource_service")
		}
	}
	if filter.ResourceCategory != "" {
		clauses = append(clauses, "n.resource_category = $resource_category")
	}
	if filter.scoped() {
		scalars, _ := scopeGrantInlineScalars(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs)
		clauses = append(clauses, infraResourceScopePredicate("n", scalars))
	}
	return clauses
}

func infraResourceAggregateParams(filter InfraResourceAggregateFilter) map[string]any {
	params := map[string]any{}
	if filter.Kind != "" {
		params["kind"] = filter.Kind
	}
	if filter.ResourceType != "" {
		params["resource_type"] = filter.ResourceType
	}
	if filter.Provider != "" {
		params["provider"] = filter.Provider
	}
	if filter.Environment != "" {
		params["environment"] = filter.Environment
	}
	if filter.ResourceService != "" {
		params["resource_service"] = filter.ResourceService
	}
	if filter.ResourceCategory != "" {
		params["resource_category"] = filter.ResourceCategory
	}
	if filter.scoped() {
		// Bind both grant arrays unconditionally in scoped mode so the
		// $allowed_repository_ids / $allowed_scope_ids parameters referenced by
		// infraResourceScopePredicate always resolve, even when one side is
		// empty (for example a token granted only ingestion scopes).
		params["allowed_repository_ids"] = append([]string(nil), filter.AllowedRepositoryIDs...)
		params["allowed_scope_ids"] = append([]string(nil), filter.AllowedScopeIDs...)
		// Bind the per-grant SHAPE-A inline-map scalars (scope_grant_0..N) using
		// the same deterministically ordered, capped slice the predicate builder
		// references, so keys and count agree exactly.
		scalars, _ := scopeGrantInlineScalars(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs)
		bindScopeGrantInlineScalars(params, scalars)
	}
	return params
}

func infraResourceAggregateCloudCategory(filter InfraResourceAggregateFilter) bool {
	return strings.EqualFold(strings.TrimSpace(filter.Category), "cloud")
}

func infraResourceAggregateAllCategories(filter InfraResourceAggregateFilter) bool {
	return strings.TrimSpace(filter.Category) == ""
}

func infraResourceAggregateCanReachCloud(filter InfraResourceAggregateFilter) bool {
	return infraResourceAggregateCloudCategory(filter) || infraResourceAggregateAllCategories(filter)
}

func infraResourceProviderGroupExpression(filter InfraResourceAggregateFilter) string {
	if infraResourceAggregateCloudCategory(filter) {
		return "CASE WHEN n.source_system IS NULL OR n.source_system = '' THEN 'unknown' ELSE n.source_system END"
	}
	if infraResourceAggregateAllCategories(filter) {
		// Flat single-level CASE. The all-categories read groups CloudResource
		// (whose provider signal lives in `source_system`) alongside IaC labels
		// (whose provider signal lives in `provider`), so a provider-less node
		// falls back to `source_system` only when it is a CloudResource, else
		// buckets as `unknown`.
		//
		// The CloudResource gate uses `'CloudResource' IN labels(n)`, not a
		// `n:CloudResource` label test. On the pinned NornicDB build a label
		// test inside a CASE/projection is echoed as the literal expression
		// text, so a nested `CASE WHEN n:CloudResource THEN ... END` collapsed
		// every provider-less node into a null bucket and mis-bucketed even
		// nodes carrying a real provider (#5283). `IN labels(n)` evaluates
		// correctly, and flattening the nested CASE avoids the CASE-in-CASE
		// shape the same build mangles. The `coalesce(prop, '') <> ''` guards
		// treat NULL and empty-string identically, matching the prior intent.
		return "CASE WHEN coalesce(n.provider, '') <> '' THEN n.provider WHEN ('CloudResource' IN labels(n)) AND coalesce(n.source_system, '') <> '' THEN n.source_system ELSE 'unknown' END"
	}
	return "CASE WHEN n.provider IS NULL OR n.provider = '' THEN 'unknown' ELSE n.provider END"
}

func infraResourceServiceGroupExpression(filter InfraResourceAggregateFilter) string {
	if infraResourceAggregateCloudCategory(filter) {
		return "CASE WHEN n.service_kind IS NULL OR n.service_kind = '' THEN 'unknown' ELSE n.service_kind END"
	}
	if infraResourceAggregateAllCategories(filter) {
		return "CASE WHEN coalesce(n.resource_service, n.service_kind, '') = '' THEN 'unknown' ELSE coalesce(n.resource_service, n.service_kind, '') END"
	}
	return "CASE WHEN n.resource_service IS NULL OR n.resource_service = '' THEN 'unknown' ELSE n.resource_service END"
}

// infraResourceInventoryGroupExpression maps the dimension enum to the safe
// Cypher property reference substituted into the inventory query. Only
// known enum values are accepted, so the substitution stays parameter-safe;
// filter values flow through bound parameters only. The CASE expression
// maps both NULL and empty-string to `unknown` so a missing optional
// property never surfaces as `""` in the bucket key.
func infraResourceInventoryGroupExpression(
	dimension InfraResourceInventoryDimension,
	filter InfraResourceAggregateFilter,
) (string, error) {
	switch dimension {
	case InfraResourceInventoryByProvider:
		return infraResourceProviderGroupExpression(filter), nil
	case InfraResourceInventoryByEnvironment:
		return "CASE WHEN n.environment IS NULL OR n.environment = '' THEN 'unknown' ELSE n.environment END", nil
	case InfraResourceInventoryByResourceCategory:
		return "CASE WHEN n.resource_category IS NULL OR n.resource_category = '' THEN 'unknown' ELSE n.resource_category END", nil
	case InfraResourceInventoryByResourceService:
		return infraResourceServiceGroupExpression(filter), nil
	case InfraResourceInventoryByLabel:
		// `labels(n)` is a small list (1-2 labels per node); head() picks the
		// canonical primary label. No empty-string normalization needed since
		// every node has at least one label by definition.
		return "head(labels(n))", nil
	default:
		return "", fmt.Errorf("unsupported infra resource inventory dimension: %q", dimension)
	}
}
