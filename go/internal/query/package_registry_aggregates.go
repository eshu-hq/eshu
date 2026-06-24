// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

// PackageRegistryAggregateStore reads cheap-summary aggregates over the
// graph-backed (:Package) corpus. It replaces the page-and-iterate caller
// workflow for ecosystem-level questions like "how many packages per
// ecosystem?" or "how many private packages per registry?" exposed by
// list_package_registry_packages.
//
// All reads target the same indexed label-property anchors the existing list
// endpoint already uses: (:Package){uid}, (:Package){ecosystem}, and (going
// forward) (:Package){registry,namespace,package_manager,visibility}. The
// missing indexes ship in this PR's schema migration.
type PackageRegistryAggregateStore interface {
	CountPackageRegistryPackages(context.Context, PackageRegistryAggregateFilter) (PackageRegistryAggregateCount, error)
	PackageRegistryPackageInventory(
		context.Context,
		PackageRegistryAggregateFilter,
		PackageRegistryInventoryDimension,
		int,
		int,
	) ([]PackageRegistryInventoryRow, error)
}

// PackageRegistryInventoryDimension names the grouping dimension for the
// inventory aggregate. Each enum value names a (:Package) property that has
// (or, in this PR's migration, gains) a graph index so the Cypher resolves
// to a cookbook Area-5 grouped-count hot path instead of a label scan.
type PackageRegistryInventoryDimension string

const (
	// PackageRegistryInventoryByEcosystem groups by (:Package).ecosystem.
	// Backed by the long-standing `package_ecosystem` index.
	PackageRegistryInventoryByEcosystem PackageRegistryInventoryDimension = "ecosystem"
	// PackageRegistryInventoryByRegistry groups by (:Package).registry.
	// Requires the `package_registry` index added in this PR.
	PackageRegistryInventoryByRegistry PackageRegistryInventoryDimension = "registry"
	// PackageRegistryInventoryByNamespace groups by (:Package).namespace.
	// Requires the `package_namespace` index added in this PR.
	PackageRegistryInventoryByNamespace PackageRegistryInventoryDimension = "namespace"
	// PackageRegistryInventoryByPackageManager groups by
	// (:Package).package_manager. Requires the `package_package_manager`
	// index added in this PR.
	PackageRegistryInventoryByPackageManager PackageRegistryInventoryDimension = "package_manager"
	// PackageRegistryInventoryByVisibility groups by (:Package).visibility.
	// Requires the `package_visibility` index added in this PR.
	PackageRegistryInventoryByVisibility PackageRegistryInventoryDimension = "visibility"
)

// PackageRegistryAggregateMaxLimit caps inventory result pages.
const PackageRegistryAggregateMaxLimit = 500

// PackageRegistryAggregateFilter narrows aggregate reads. An aggregate
// without a scope is allowed because the dataset is already bounded to the
// (:Package) label; the cookbook treats `MATCH (n:Label) WHERE indexed_prop
// = $v RETURN count(*)` as the hot-path shape, and any single filter we
// expose anchors on a property that has an index after this PR's migration.
type PackageRegistryAggregateFilter struct {
	Ecosystem      string
	Registry       string
	Namespace      string
	PackageManager string
	Visibility     string
}

// PackageRegistryAggregateCount is the cheap-summary totals envelope used by
// the count handler. The single rollup `ByEcosystem` runs on the existing
// `package_ecosystem` index — the cheapest grouped scan available — so the
// count call stays bounded to two indexed reads. Callers asking for
// per-registry / per-namespace / per-package_manager / per-visibility
// breakdowns use the inventory endpoint, where `group_by` selects exactly
// one dimension and one Cypher round trip.
type PackageRegistryAggregateCount struct {
	TotalPackages int
	ByEcosystem   map[string]int
}

// PackageRegistryInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type PackageRegistryInventoryRow struct {
	Dimension PackageRegistryInventoryDimension `json:"dimension"`
	Value     string                            `json:"value"`
	Count     int                               `json:"count"`
}

// GraphPackageRegistryAggregateStore reads aggregate counts via the
// `GraphQuery` port. Decoupling from the concrete `Neo4jReader` lets the
// handler tests inject an in-memory stub that asserts on the Cypher shape
// and parameter bag the production code sends.
type GraphPackageRegistryAggregateStore struct {
	Graph GraphQuery
}

// NewGraphPackageRegistryAggregateStore wires a GraphQuery (Neo4jReader in
// production) into the package-registry aggregate Reader.
func NewGraphPackageRegistryAggregateStore(graph GraphQuery) GraphPackageRegistryAggregateStore {
	return GraphPackageRegistryAggregateStore{Graph: graph}
}

// Cypher templates intentionally stay close to the cookbook Area-5 shape:
//
//	MATCH (n:Label) WHERE n.indexed_prop = $value
//	RETURN <group_expr> AS bucket, count(*) AS bucket_count
//	ORDER BY bucket_count DESC
//	[SKIP $offset] LIMIT $limit
//
// The optional scope filters use the `coalesce`-free `($x = '' OR p.x = $x)`
// pattern that the existing list handler relies on (package_registry.go).
// Each filter property is indexed after this PR's schema migration, so the
// planner can pick the most selective anchor.

const packageRegistryAggregateCountQuery = `
MATCH (p:Package)
WHERE ($ecosystem = '' OR p.ecosystem = $ecosystem)
  AND ($registry = '' OR p.registry = $registry)
  AND ($namespace = '' OR p.namespace = $namespace)
  AND ($package_manager = '' OR p.package_manager = $package_manager)
  AND ($visibility = '' OR p.visibility = $visibility)
RETURN count(p) AS total
`

// Bucket normalization uses a CASE expression instead of a plain coalesce so
// that empty-string properties (commonly seen on optional fields like
// `namespace` for ecosystems without a namespace concept) map to `unknown`
// alongside genuine NULLs. A plain `coalesce(p.<prop>, 'unknown')` would
// emit `""` as a bucket key because Cypher coalesce only collapses NULLs,
// not empty strings.
const packageRegistryAggregateByEcosystemQuery = `
MATCH (p:Package)
WHERE ($ecosystem = '' OR p.ecosystem = $ecosystem)
  AND ($registry = '' OR p.registry = $registry)
  AND ($namespace = '' OR p.namespace = $namespace)
  AND ($package_manager = '' OR p.package_manager = $package_manager)
  AND ($visibility = '' OR p.visibility = $visibility)
RETURN CASE WHEN p.ecosystem IS NULL OR p.ecosystem = '' THEN 'unknown' ELSE p.ecosystem END AS bucket,
       count(p) AS bucket_count
ORDER BY bucket_count DESC, bucket
`

const packageRegistryInventoryQueryTemplate = `
MATCH (p:Package)
WHERE ($ecosystem = '' OR p.ecosystem = $ecosystem)
  AND ($registry = '' OR p.registry = $registry)
  AND ($namespace = '' OR p.namespace = $namespace)
  AND ($package_manager = '' OR p.package_manager = $package_manager)
  AND ($visibility = '' OR p.visibility = $visibility)
RETURN CASE WHEN %s IS NULL OR %s = '' THEN 'unknown' ELSE %s END AS bucket,
       count(p) AS bucket_count
ORDER BY bucket_count DESC, bucket
SKIP $offset
LIMIT $limit
`

func packageRegistryAggregateParams(filter PackageRegistryAggregateFilter) map[string]any {
	return map[string]any{
		"ecosystem":       filter.Ecosystem,
		"registry":        filter.Registry,
		"namespace":       filter.Namespace,
		"package_manager": filter.PackageManager,
		"visibility":      filter.Visibility,
	}
}

// CountPackageRegistryPackages returns the cheap-summary totals envelope for
// the scoped (:Package) slice.
func (s GraphPackageRegistryAggregateStore) CountPackageRegistryPackages(
	ctx context.Context,
	filter PackageRegistryAggregateFilter,
) (PackageRegistryAggregateCount, error) {
	if s.Graph == nil {
		return PackageRegistryAggregateCount{}, fmt.Errorf("package registry aggregate graph is required")
	}

	params := packageRegistryAggregateParams(filter)

	totalRows, err := s.Graph.Run(ctx, packageRegistryAggregateCountQuery, params)
	if err != nil {
		return PackageRegistryAggregateCount{}, fmt.Errorf("count package registry packages: %w", err)
	}
	var total int
	if len(totalRows) > 0 {
		total = IntVal(totalRows[0], "total")
	}

	out := PackageRegistryAggregateCount{
		TotalPackages: total,
		ByEcosystem:   map[string]int{},
	}

	rows, err := s.Graph.Run(ctx, packageRegistryAggregateByEcosystemQuery, params)
	if err != nil {
		return PackageRegistryAggregateCount{}, fmt.Errorf("group package registry packages by ecosystem: %w", err)
	}
	for _, row := range rows {
		bucket := StringVal(row, "bucket")
		count := IntVal(row, "bucket_count")
		out.ByEcosystem[bucket] = count
	}
	return out, nil
}

// PackageRegistryPackageInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s GraphPackageRegistryAggregateStore) PackageRegistryPackageInventory(
	ctx context.Context,
	filter PackageRegistryAggregateFilter,
	dimension PackageRegistryInventoryDimension,
	limit int,
	offset int,
) ([]PackageRegistryInventoryRow, error) {
	if s.Graph == nil {
		return nil, fmt.Errorf("package registry aggregate graph is required")
	}
	groupExpr, err := packageRegistryInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > PackageRegistryAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", PackageRegistryAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	// The template repeats the dimension property three times in the CASE
	// expression (NULL check, empty-string check, ELSE branch) so a single
	// validated `groupExpr` populates all three slots; substitution stays
	// safe because the closed enum is the only caller of this Sprintf.
	cypher := fmt.Sprintf(packageRegistryInventoryQueryTemplate, groupExpr, groupExpr, groupExpr)
	params := packageRegistryAggregateParams(filter)
	params["limit"] = limit
	params["offset"] = offset

	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("inventory package registry packages: %w", err)
	}
	out := make([]PackageRegistryInventoryRow, 0, limit)
	for _, row := range rows {
		bucket := strings.TrimSpace(StringVal(row, "bucket"))
		count := IntVal(row, "bucket_count")
		out = append(out, PackageRegistryInventoryRow{
			Dimension: dimension,
			Value:     bucket,
			Count:     count,
		})
	}
	return out, nil
}

// packageRegistryInventoryGroupExpression maps the dimension enum to the
// safe Cypher property reference substituted into the inventory query
// template. Only known enum values are accepted, so the substitution stays
// parameter-safe; user-supplied filter values flow through bound parameters
// only.
func packageRegistryInventoryGroupExpression(
	dimension PackageRegistryInventoryDimension,
) (string, error) {
	switch dimension {
	case PackageRegistryInventoryByEcosystem:
		return "p.ecosystem", nil
	case PackageRegistryInventoryByRegistry:
		return "p.registry", nil
	case PackageRegistryInventoryByNamespace:
		return "p.namespace", nil
	case PackageRegistryInventoryByPackageManager:
		return "p.package_manager", nil
	case PackageRegistryInventoryByVisibility:
		return "p.visibility", nil
	default:
		return "", fmt.Errorf("unsupported package registry inventory dimension: %q", dimension)
	}
}
