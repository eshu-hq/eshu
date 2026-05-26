package query

import (
	"context"
	"fmt"
	"strings"
)

// InfraResourceAggregateStore reads cheap-summary aggregates over the
// graph-backed infrastructure resource corpus (TerraformResource,
// K8sResource, CloudFormationResource, ArgoCDApplication, CrossplaneXRD,
// HelmChart, and friends — see `allInfraLabels` in infra.go). It replaces
// the page-and-iterate caller workflow for ecosystem-level questions like
// "how many resources per provider?" or "how many Terraform resources per
// account?" exposed by find_infra_resources.
//
// Performance contract: hot-path eligibility requires a label-property
// anchor with an indexed property. The list endpoint and this aggregate
// both `MATCH (n)` filtered by an OR of the documented infrastructure
// labels (see `infraLabelPredicate`). The `category` filter narrows the
// label set to one of {k8s, terraform, argocd, crossplane, helm}; when
// combined with an indexed property predicate that label exposes, the
// aggregate hits the cookbook Area-5 hot path. Without a category filter
// the aggregate falls back to a label-set scan. The PR description names
// the operator PROFILE gate.
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
type InfraResourceAggregateFilter struct {
	Category         string
	Kind             string
	ResourceType     string
	Provider         string
	Environment      string
	ResourceService  string
	ResourceCategory string
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

	whereClause := infraResourceAggregateWhereClause(labels, filter)
	params := infraResourceAggregateParams(filter)

	totalRows, err := s.Graph.Run(ctx,
		"MATCH (n) "+whereClause+" RETURN count(n) AS total", params)
	if err != nil {
		return InfraResourceAggregateCount{}, fmt.Errorf("count infra resources: %w", err)
	}
	var total int
	if len(totalRows) > 0 {
		total = IntVal(totalRows[0], "total")
	}

	out := InfraResourceAggregateCount{
		TotalResources: total,
		ByProvider:     map[string]int{},
		ByEnvironment:  map[string]int{},
		ByLabel:        map[string]int{},
	}
	if err := s.fillBuckets(ctx, whereClause, params,
		"CASE WHEN n.provider IS NULL OR n.provider = '' THEN 'unknown' ELSE n.provider END",
		out.ByProvider); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, whereClause, params,
		"CASE WHEN n.environment IS NULL OR n.environment = '' THEN 'unknown' ELSE n.environment END",
		out.ByEnvironment); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	// Group by the node's primary label. `labels(n)` returns a list; we
	// surface the first label, which is the canonical type for these nodes.
	if err := s.fillBuckets(ctx, whereClause, params,
		"head(labels(n))",
		out.ByLabel); err != nil {
		return InfraResourceAggregateCount{}, err
	}
	return out, nil
}

func (s GraphInfraResourceAggregateStore) fillBuckets(
	ctx context.Context,
	whereClause string,
	params map[string]any,
	groupExpr string,
	dst map[string]int,
) error {
	cypher := "MATCH (n) " + whereClause +
		" RETURN " + groupExpr + " AS bucket, count(n) AS bucket_count" +
		" ORDER BY bucket_count DESC, bucket"
	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("group infra resources: %w", err)
	}
	for _, row := range rows {
		bucket := strings.TrimSpace(StringVal(row, "bucket"))
		count := IntVal(row, "bucket_count")
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
	groupExpr, err := infraResourceInventoryGroupExpression(dimension)
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

	whereClause := infraResourceAggregateWhereClause(labels, filter)
	params := infraResourceAggregateParams(filter)
	params["limit"] = limit
	params["offset"] = offset

	cypher := "MATCH (n) " + whereClause +
		" RETURN " + groupExpr + " AS bucket, count(n) AS bucket_count" +
		" ORDER BY bucket_count DESC, bucket" +
		" SKIP $offset LIMIT $limit"

	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("inventory infra resources: %w", err)
	}
	out := make([]InfraResourceInventoryRow, 0, limit)
	for _, row := range rows {
		bucket := strings.TrimSpace(StringVal(row, "bucket"))
		count := IntVal(row, "bucket_count")
		out = append(out, InfraResourceInventoryRow{
			Dimension: dimension,
			Value:     bucket,
			Count:     count,
		})
	}
	return out, nil
}

// infraResourceAggregateWhereClause renders the label predicate and the
// optional indexed-property filters. Filter values flow through bound
// parameters; only the label list is interpolated, and it comes from the
// closed `allInfraLabels` / `infraCategoryLabels` enums (no user input).
//
// Property predicates use direct equality (`n.provider = $provider`) rather
// than `coalesce(n.provider, '') = $provider`. The clauses only render when
// the caller passed a non-empty filter value, so the coalesce-wrapped form
// is semantically equivalent to direct equality (Cypher equality is
// null-rejecting). Direct equality keeps the predicate eligible for the
// `tf_resource_provider` / `tf_resource_environment` / `tf_resource_service`
// / `tf_resource_category` indexes on TerraformResource; the coalesce
// wrapper would block planner index selection.
func infraResourceAggregateWhereClause(labels []string, filter InfraResourceAggregateFilter) string {
	clauses := []string{infraLabelPredicate(labels)}
	if filter.Kind != "" {
		clauses = append(clauses, "(n.kind = $kind OR n.resource_type = $kind OR n.data_type = $kind)")
	}
	if filter.ResourceType != "" {
		clauses = append(clauses, "(n.resource_type = $resource_type OR n.data_type = $resource_type)")
	}
	if filter.Provider != "" {
		clauses = append(clauses, "n.provider = $provider")
	}
	if filter.Environment != "" {
		clauses = append(clauses, "n.environment = $environment")
	}
	if filter.ResourceService != "" {
		clauses = append(clauses, "n.resource_service = $resource_service")
	}
	if filter.ResourceCategory != "" {
		clauses = append(clauses, "n.resource_category = $resource_category")
	}
	return "WHERE " + strings.Join(clauses, " AND ")
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
	return params
}

// infraResourceInventoryGroupExpression maps the dimension enum to the safe
// Cypher property reference substituted into the inventory query. Only
// known enum values are accepted, so the substitution stays parameter-safe;
// filter values flow through bound parameters only. The CASE expression
// maps both NULL and empty-string to `unknown` so a missing optional
// property never surfaces as `""` in the bucket key.
func infraResourceInventoryGroupExpression(
	dimension InfraResourceInventoryDimension,
) (string, error) {
	switch dimension {
	case InfraResourceInventoryByProvider:
		return "CASE WHEN n.provider IS NULL OR n.provider = '' THEN 'unknown' ELSE n.provider END", nil
	case InfraResourceInventoryByEnvironment:
		return "CASE WHEN n.environment IS NULL OR n.environment = '' THEN 'unknown' ELSE n.environment END", nil
	case InfraResourceInventoryByResourceCategory:
		return "CASE WHEN n.resource_category IS NULL OR n.resource_category = '' THEN 'unknown' ELSE n.resource_category END", nil
	case InfraResourceInventoryByResourceService:
		return "CASE WHEN n.resource_service IS NULL OR n.resource_service = '' THEN 'unknown' ELSE n.resource_service END", nil
	case InfraResourceInventoryByLabel:
		// `labels(n)` is a small list (1-2 labels per node); head() picks the
		// canonical primary label. No empty-string normalization needed since
		// every node has at least one label by definition.
		return "head(labels(n))", nil
	default:
		return "", fmt.Errorf("unsupported infra resource inventory dimension: %q", dimension)
	}
}
