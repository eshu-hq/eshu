// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// Filter narrows a table read. Labels is the already-resolved subset of Labels
// to read. An empty Labels reads nothing. AllCategories selects the
// all-categories predicate variants the graph readers use when no category
// filter is set. Every other field is an equality filter that applies only
// when non-empty. Each clause mirrors infraResourceAggregateFilterClauses in
// the query package for entity-derived labels.
type Filter struct {
	Labels           []string
	AllCategories    bool
	Kind             string
	ResourceType     string
	Provider         string
	Environment      string
	ResourceService  string
	ResourceCategory string

	// repoIDs restricts the read to some repositories. Only tests set it; see
	// export_test.go.
	repoIDs []string
}

// Dimension is one grouping column of DimensionBuckets.
type Dimension string

const (
	// DimensionProvider groups by provider, empty as "unknown".
	DimensionProvider Dimension = "provider"
	// DimensionEnvironment groups by environment, empty as "unknown".
	DimensionEnvironment Dimension = "environment"
	// DimensionResourceCategory groups by resource_category, empty as "unknown".
	DimensionResourceCategory Dimension = "resource_category"
	// DimensionResourceService groups by resource_service. For all-categories
	// reads it falls back to service_kind, and empty is "unknown".
	DimensionResourceService Dimension = "resource_service"
	// DimensionLabel groups by the canonical graph label.
	DimensionLabel Dimension = "label"
)

// CountBucket is one (label, provider bucket, environment bucket) group.
type CountBucket struct {
	Label       string
	Provider    string
	Environment string
	Count       int64
}

const (
	providerBucketSQL    = `CASE WHEN provider = '' THEN 'unknown' ELSE provider END`
	environmentBucketSQL = `CASE WHEN environment = '' THEN 'unknown' ELSE environment END`
	categoryBucketSQL    = `CASE WHEN resource_category = '' THEN 'unknown' ELSE resource_category END`
	serviceBucketSQL     = `CASE WHEN resource_service = '' THEN 'unknown' ELSE resource_service END`
	// serviceAllCategoriesBucketSQL mirrors the graph expression
	// coalesce(n.resource_service, n.service_kind, ''): the graph never stores
	// an empty string, so NULLIF restores its missing-property semantics.
	serviceAllCategoriesBucketSQL = `COALESCE(NULLIF(resource_service, ''), NULLIF(service_kind, ''), 'unknown')`
)

// CountBuckets returns the (label, provider, environment) groups for filter.
// The count route derives its total and all three rollups from these rows in
// one statement: the entities aggregate for content-derived labels UNION ALL
// the fact aggregates for graph-only labels (#6843). The label sets are
// disjoint, so rows never overlap and no outer grouping is needed.
func CountBuckets(ctx context.Context, queryer db.Queryer, filter Filter) ([]CountBucket, error) {
	entityLabels, factsLabels := partitionReadLabels(filter.Labels)
	if len(entityLabels)+len(factsLabels) == 0 {
		return nil, nil
	}
	var parts []string
	var args []any
	if len(entityLabels) > 0 {
		entityFilter := filter
		entityFilter.Labels = entityLabels
		where, whereArgs := entityFilter.whereClause()
		args = append(args, whereArgs...)
		parts = append(parts, `SELECT label, `+providerBucketSQL+`, `+environmentBucketSQL+`, count(*)
FROM infra_resource_entities
WHERE `+where+`
GROUP BY 1, 2, 3`)
	}
	for _, label := range factsLabels {
		var branch string
		branch, args = graphOnlyCountBranch(label, filter, args)
		parts = append(parts, branch)
	}
	query := graphOnlyCTEs(factsLabels) + strings.Join(parts, "\nUNION ALL\n")
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count infra inventory buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CountBucket
	for rows.Next() {
		var bucket CountBucket
		if err := rows.Scan(&bucket.Label, &bucket.Provider, &bucket.Environment, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan infra inventory bucket: %w", err)
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count infra inventory buckets: %w", err)
	}
	return out, nil
}

// DimensionBuckets returns the row count per value of dimension for filter.
func DimensionBuckets(ctx context.Context, queryer db.Queryer, filter Filter, dimension Dimension) (map[string]int64, error) {
	bucketSQL, err := dimensionBucketSQL(dimension, filter.AllCategories)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	entityLabels, factsLabels := partitionReadLabels(filter.Labels)
	if len(entityLabels)+len(factsLabels) == 0 {
		return out, nil
	}
	var parts []string
	var args []any
	if len(entityLabels) > 0 {
		entityFilter := filter
		entityFilter.Labels = entityLabels
		where, whereArgs := entityFilter.whereClause()
		args = append(args, whereArgs...)
		parts = append(parts, `SELECT `+bucketSQL+`, count(*)
FROM infra_resource_entities
WHERE `+where+`
GROUP BY 1`)
	}
	for _, label := range factsLabels {
		var branch string
		branch, args = graphOnlyDimensionBranch(label, filter, dimension, args)
		parts = append(parts, branch)
	}
	query := graphOnlyCTEs(factsLabels) + strings.Join(parts, "\nUNION ALL\n")
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("group infra inventory by %s: %w", dimension, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value string
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			return nil, fmt.Errorf("scan infra inventory %s bucket: %w", dimension, err)
		}
		out[value] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("group infra inventory by %s: %w", dimension, err)
	}
	return out, nil
}

func dimensionBucketSQL(dimension Dimension, allCategories bool) (string, error) {
	switch dimension {
	case DimensionProvider:
		return providerBucketSQL, nil
	case DimensionEnvironment:
		return environmentBucketSQL, nil
	case DimensionResourceCategory:
		return categoryBucketSQL, nil
	case DimensionResourceService:
		if allCategories {
			return serviceAllCategoriesBucketSQL, nil
		}
		return serviceBucketSQL, nil
	case DimensionLabel:
		return "label", nil
	default:
		return "", fmt.Errorf("unsupported infra inventory dimension %q", dimension)
	}
}

// whereClause renders the label restriction plus every non-empty filter as
// bound parameters. Nothing caller-supplied is interpolated.
func (f Filter) whereClause() (string, []any) {
	args := []any{pgarray.StringArray(f.Labels)}
	clauses := []string{"label = ANY($1::text[])"}
	param := func(value string) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if f.Kind != "" {
		p := param(f.Kind)
		if f.AllCategories {
			clauses = append(clauses, "(kind = "+p+" OR resource_type = "+p+" OR data_type = "+p+" OR service_kind = "+p+")")
		} else {
			clauses = append(clauses, "(kind = "+p+" OR resource_type = "+p+" OR data_type = "+p+")")
		}
	}
	if f.ResourceType != "" {
		p := param(f.ResourceType)
		clauses = append(clauses, "(resource_type = "+p+" OR data_type = "+p+")")
	}
	if f.Provider != "" {
		clauses = append(clauses, "provider = "+param(f.Provider))
	}
	if f.Environment != "" {
		clauses = append(clauses, "environment = "+param(f.Environment))
	}
	if f.ResourceService != "" {
		p := param(f.ResourceService)
		if f.AllCategories {
			clauses = append(clauses, "(resource_service = "+p+" OR service_kind = "+p+")")
		} else {
			clauses = append(clauses, "resource_service = "+p)
		}
	}
	if f.ResourceCategory != "" {
		clauses = append(clauses, "resource_category = "+param(f.ResourceCategory))
	}
	if len(f.repoIDs) > 0 {
		args = append(args, pgarray.StringArray(f.repoIDs))
		clauses = append(clauses, "repo_id = ANY($"+strconv.Itoa(len(args))+"::text[])")
	}
	return strings.Join(clauses, "\n  AND "), args
}

// Reader adapts the package-level read functions to one database, so the query
// layer can hold the read model as a single dependency.
type Reader struct {
	DB db.Queryer
}

// Ready reports whether readers may serve table counts: the backfill marker
// exists and no repository carries a fence mark (ReadModelReady).
func (r Reader) Ready(ctx context.Context) (bool, error) {
	return ReadModelReady(ctx, r.DB)
}

// CountBuckets returns CountBuckets(ctx, r.DB, filter).
func (r Reader) CountBuckets(ctx context.Context, filter Filter) ([]CountBucket, error) {
	return CountBuckets(ctx, r.DB, filter)
}

// DimensionBuckets returns DimensionBuckets(ctx, r.DB, filter, dimension).
func (r Reader) DimensionBuckets(ctx context.Context, filter Filter, dimension Dimension) (map[string]int64, error) {
	return DimensionBuckets(ctx, r.DB, filter, dimension)
}
