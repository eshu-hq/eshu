// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// CICDRunCorrelationAggregateStore reads cheap-summary aggregates over
// reducer-owned CI/CD run correlations. It replaces the page-and-iterate
// caller workflow for ecosystem-level questions like "how many runs ended in
// each outcome per environment?" or "which repositories ran the most
// pipelines this week?".
type CICDRunCorrelationAggregateStore interface {
	CountCICDRunCorrelations(context.Context, CICDRunCorrelationAggregateFilter) (CICDRunCorrelationAggregateCount, error)
	CICDRunCorrelationInventory(
		context.Context,
		CICDRunCorrelationAggregateFilter,
		CICDRunCorrelationInventoryDimension,
		int,
		int,
	) ([]CICDRunCorrelationInventoryRow, error)
}

// CICDRunCorrelationInventoryDimension names the grouping dimension for the
// inventory aggregate.
type CICDRunCorrelationInventoryDimension string

const (
	// CICDRunCorrelationInventoryByOutcome groups by reducer outcome
	// (exact / derived / ambiguous / unresolved / rejected).
	CICDRunCorrelationInventoryByOutcome CICDRunCorrelationInventoryDimension = "outcome"
	// CICDRunCorrelationInventoryByEnvironment groups by deployment
	// environment.
	CICDRunCorrelationInventoryByEnvironment CICDRunCorrelationInventoryDimension = "environment"
	// CICDRunCorrelationInventoryByRepository groups by repository_id.
	CICDRunCorrelationInventoryByRepository CICDRunCorrelationInventoryDimension = "repository_id"
	// CICDRunCorrelationInventoryByProvider groups by CI provider.
	CICDRunCorrelationInventoryByProvider CICDRunCorrelationInventoryDimension = "provider"
)

// CICDRunCorrelationAggregateMaxLimit caps inventory result pages.
const CICDRunCorrelationAggregateMaxLimit = 500

// CICDRunCorrelationAggregateFilter narrows aggregate reads. An aggregate
// without a scope is allowed because the totals question itself is the call
// shape we want to support — the dataset is already bounded by `fact_kind`
// and the active-generation predicate at index lookup time.
type CICDRunCorrelationAggregateFilter struct {
	ScopeID              string
	RepositoryID         string
	CommitSHA            string
	Provider             string
	ArtifactDigest       string
	ImageRef             string
	Environment          string
	Outcome              string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// CICDRunCorrelationAggregateCount is the cheap-summary totals envelope used
// by the count handler. ByOutcome / ByEnvironment / ByProvider are
// pre-aggregated rollups so callers can answer "runs per outcome",
// "runs per environment", and "runs per provider" without a second round trip.
type CICDRunCorrelationAggregateCount struct {
	TotalCorrelations int
	ByOutcome         map[string]int
	ByEnvironment     map[string]int
	ByProvider        map[string]int
}

// CICDRunCorrelationInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type CICDRunCorrelationInventoryRow struct {
	Dimension CICDRunCorrelationInventoryDimension `json:"dimension"`
	Value     string                               `json:"value"`
	Count     int                                  `json:"count"`
}

// PostgresCICDRunCorrelationAggregateStore reads aggregate counts directly
// from reducer-owned CI/CD run correlation facts.
type PostgresCICDRunCorrelationAggregateStore struct {
	DB cicdRunCorrelationAggregateQueryer
}

type cicdRunCorrelationAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresCICDRunCorrelationAggregateStore creates the Postgres-backed
// aggregate store.
func NewPostgresCICDRunCorrelationAggregateStore(
	db cicdRunCorrelationAggregateQueryer,
) PostgresCICDRunCorrelationAggregateStore {
	return PostgresCICDRunCorrelationAggregateStore{DB: db}
}

const cicdRunCorrelationAggregateTotalQuery = `
SELECT COUNT(*) AS total
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_ci_cd_run_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.scope_id = $1)
  AND ($2 = '' OR fact.payload->>'repository_id' = $2)
  AND ($3 = '' OR fact.payload->>'commit_sha' = $3)
  AND ($4 = '' OR fact.payload->>'provider' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_digest' = $5)
  AND ($6 = '' OR fact.payload->>'image_ref' = $6)
  AND ($7 = '' OR fact.payload->>'environment' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND (
    (COALESCE(cardinality($9::text[]), 0) = 0 AND COALESCE(cardinality($10::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($9::text[])
    OR fact.scope_id = ANY($10::text[])
  );
`

const cicdRunCorrelationAggregateGroupQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_ci_cd_run_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.scope_id = $1)
  AND ($2 = '' OR fact.payload->>'repository_id' = $2)
  AND ($3 = '' OR fact.payload->>'commit_sha' = $3)
  AND ($4 = '' OR fact.payload->>'provider' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_digest' = $5)
  AND ($6 = '' OR fact.payload->>'image_ref' = $6)
  AND ($7 = '' OR fact.payload->>'environment' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND (
    (COALESCE(cardinality($9::text[]), 0) = 0 AND COALESCE(cardinality($10::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($9::text[])
    OR fact.scope_id = ANY($10::text[])
  )
GROUP BY bucket;
`

const cicdRunCorrelationInventoryQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_ci_cd_run_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.scope_id = $1)
  AND ($2 = '' OR fact.payload->>'repository_id' = $2)
  AND ($3 = '' OR fact.payload->>'commit_sha' = $3)
  AND ($4 = '' OR fact.payload->>'provider' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_digest' = $5)
  AND ($6 = '' OR fact.payload->>'image_ref' = $6)
  AND ($7 = '' OR fact.payload->>'environment' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND (
    (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($11::text[])
    OR fact.scope_id = ANY($12::text[])
  )
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $9 OFFSET $10;
`

// CountCICDRunCorrelations returns the cheap-summary totals envelope for the
// scoped CI/CD slice.
func (s PostgresCICDRunCorrelationAggregateStore) CountCICDRunCorrelations(
	ctx context.Context,
	filter CICDRunCorrelationAggregateFilter,
) (CICDRunCorrelationAggregateCount, error) {
	if s.DB == nil {
		return CICDRunCorrelationAggregateCount{}, fmt.Errorf("ci/cd run correlation aggregate database is required")
	}

	args := []any{
		filter.ScopeID,
		filter.RepositoryID,
		filter.CommitSHA,
		filter.Provider,
		filter.ArtifactDigest,
		filter.ImageRef,
		filter.Environment,
		filter.Outcome,
		pq.Array(filter.AllowedRepositoryIDs),
		pq.Array(filter.AllowedScopeIDs),
	}

	row := s.DB.QueryRowContext(ctx, cicdRunCorrelationAggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return CICDRunCorrelationAggregateCount{}, fmt.Errorf("count ci/cd run correlations: %w", err)
	}

	out := CICDRunCorrelationAggregateCount{
		TotalCorrelations: int(total.Int64),
		ByOutcome:         map[string]int{},
		ByEnvironment:     map[string]int{},
		ByProvider:        map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'outcome', ''), 'unknown')", out.ByOutcome); err != nil {
		return CICDRunCorrelationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'environment', ''), 'unknown')", out.ByEnvironment); err != nil {
		return CICDRunCorrelationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'provider', ''), 'unknown')", out.ByProvider); err != nil {
		return CICDRunCorrelationAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresCICDRunCorrelationAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(cicdRunCorrelationAggregateGroupQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("group ci/cd run correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan ci/cd run correlation bucket: %w", err)
		}
		dst[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// CICDRunCorrelationInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s PostgresCICDRunCorrelationAggregateStore) CICDRunCorrelationInventory(
	ctx context.Context,
	filter CICDRunCorrelationAggregateFilter,
	dimension CICDRunCorrelationInventoryDimension,
	limit int,
	offset int,
) ([]CICDRunCorrelationInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("ci/cd run correlation aggregate database is required")
	}
	groupExpr, err := cicdRunCorrelationInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > CICDRunCorrelationAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", CICDRunCorrelationAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(cicdRunCorrelationInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.ScopeID,
		filter.RepositoryID,
		filter.CommitSHA,
		filter.Provider,
		filter.ArtifactDigest,
		filter.ImageRef,
		filter.Environment,
		filter.Outcome,
		limit,
		offset,
		pq.Array(filter.AllowedRepositoryIDs),
		pq.Array(filter.AllowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("inventory ci/cd run correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]CICDRunCorrelationInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan ci/cd run correlation inventory row: %w", err)
		}
		out = append(out, CICDRunCorrelationInventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ci/cd run correlation inventory rows: %w", err)
	}
	return out, nil
}

// cicdRunCorrelationInventoryGroupExpression maps the dimension enum to the
// safe SQL expression substituted into the inventory query template. Only
// known enum values are accepted, so the substitution stays parameter-safe;
// filter values flow through bound parameters only.
func cicdRunCorrelationInventoryGroupExpression(
	dimension CICDRunCorrelationInventoryDimension,
) (string, error) {
	switch dimension {
	case CICDRunCorrelationInventoryByOutcome:
		return "COALESCE(NULLIF(fact.payload->>'outcome', ''), 'unknown')", nil
	case CICDRunCorrelationInventoryByEnvironment:
		return "COALESCE(NULLIF(fact.payload->>'environment', ''), 'unknown')", nil
	case CICDRunCorrelationInventoryByRepository:
		return "COALESCE(NULLIF(fact.payload->>'repository_id', ''), 'unknown')", nil
	case CICDRunCorrelationInventoryByProvider:
		return "COALESCE(NULLIF(fact.payload->>'provider', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported ci/cd run correlation inventory dimension: %q", dimension)
	}
}

// isSupportedCICDRunCorrelationOutcome rejects unknown outcome filters,
// matching the enum the existing list endpoint advertises in
// openapi_paths_cicd.go (`exact`, `derived`, `ambiguous`, `unresolved`,
// `rejected`).
func isSupportedCICDRunCorrelationOutcome(outcome string) bool {
	switch outcome {
	case "exact", "derived", "ambiguous", "unresolved", "rejected":
		return true
	default:
		return false
	}
}
