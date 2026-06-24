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

// ContainerImageIdentityAggregateStore reads cheap-summary aggregates over
// reducer-owned container image identities. It replaces the page-and-iterate
// caller workflow for ecosystem-level questions like "how many images
// resolved by exact digest vs tag?" or "which repositories have the most
// container images?".
type ContainerImageIdentityAggregateStore interface {
	CountContainerImageIdentities(context.Context, ContainerImageIdentityAggregateFilter) (ContainerImageIdentityAggregateCount, error)
	ContainerImageIdentityInventory(
		context.Context,
		ContainerImageIdentityAggregateFilter,
		ContainerImageIdentityInventoryDimension,
		int,
		int,
	) ([]ContainerImageIdentityInventoryRow, error)
}

// ContainerImageIdentityInventoryDimension names the grouping dimension for
// the inventory aggregate.
type ContainerImageIdentityInventoryDimension string

const (
	// ContainerImageIdentityInventoryByOutcome groups by reducer outcome
	// (exact_digest / tag_resolved).
	ContainerImageIdentityInventoryByOutcome ContainerImageIdentityInventoryDimension = "outcome"
	// ContainerImageIdentityInventoryByIdentityStrength groups by reducer
	// identity_strength.
	ContainerImageIdentityInventoryByIdentityStrength ContainerImageIdentityInventoryDimension = "identity_strength"
	// ContainerImageIdentityInventoryByRepository groups by repository_id.
	ContainerImageIdentityInventoryByRepository ContainerImageIdentityInventoryDimension = "repository_id"
)

// ContainerImageIdentityAggregateMaxLimit caps inventory result pages.
const ContainerImageIdentityAggregateMaxLimit = 500

// ContainerImageIdentityAggregateFilter narrows aggregate reads. An aggregate
// without a scope is allowed because the totals question itself is the call
// shape we want to support — the dataset is already bounded by `fact_kind`
// and the active-generation predicate at index lookup time.
type ContainerImageIdentityAggregateFilter struct {
	Digest             string
	ImageRef           string
	SourceRepositoryID string
	RepositoryID       string
	Outcome            string
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (the union
	// of granted repository and ingestion-scope ids). Empty means unrestricted;
	// when populated the aggregate counts and inventory buckets cover only
	// identities whose source_repository_ids overlap the granted set, so
	// uncorrelated images never inflate a scoped caller's totals.
	AllowedSourceRepositoryIDs []string
}

// ContainerImageIdentityAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByOutcome and ByIdentityStrength are
// pre-aggregated rollups so callers can answer "images per outcome" and
// "images per identity strength" without a second round trip.
type ContainerImageIdentityAggregateCount struct {
	TotalIdentities    int
	ByOutcome          map[string]int
	ByIdentityStrength map[string]int
}

// ContainerImageIdentityInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type ContainerImageIdentityInventoryRow struct {
	Dimension ContainerImageIdentityInventoryDimension `json:"dimension"`
	Value     string                                   `json:"value"`
	Count     int                                      `json:"count"`
}

// PostgresContainerImageIdentityAggregateStore reads aggregate counts directly
// from reducer-owned container image identity facts.
type PostgresContainerImageIdentityAggregateStore struct {
	DB containerImageIdentityAggregateQueryer
}

type containerImageIdentityAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresContainerImageIdentityAggregateStore creates the Postgres-backed
// aggregate store.
func NewPostgresContainerImageIdentityAggregateStore(
	db containerImageIdentityAggregateQueryer,
) PostgresContainerImageIdentityAggregateStore {
	return PostgresContainerImageIdentityAggregateStore{DB: db}
}

const containerImageIdentityAggregateTotalQuery = `
SELECT COUNT(*) AS total
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'digest' = $1)
  AND ($2 = '' OR fact.payload->>'image_ref' = $2)
  AND ($3 = '' OR fact.payload->'source_repository_ids' ? $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'outcome' = $5)
  AND (
        COALESCE(cardinality($6::text[]), 0) = 0
        OR fact.payload->'source_repository_ids' ?| $6::text[]
      );
`

const containerImageIdentityAggregateGroupQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'digest' = $1)
  AND ($2 = '' OR fact.payload->>'image_ref' = $2)
  AND ($3 = '' OR fact.payload->'source_repository_ids' ? $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'outcome' = $5)
  AND (
        COALESCE(cardinality($6::text[]), 0) = 0
        OR fact.payload->'source_repository_ids' ?| $6::text[]
      )
GROUP BY bucket;
`

const containerImageIdentityInventoryQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_container_image_identity'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'digest' = $1)
  AND ($2 = '' OR fact.payload->>'image_ref' = $2)
  AND ($3 = '' OR fact.payload->'source_repository_ids' ? $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'outcome' = $5)
  AND (
        COALESCE(cardinality($6::text[]), 0) = 0
        OR fact.payload->'source_repository_ids' ?| $6::text[]
      )
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $7 OFFSET $8;
`

// CountContainerImageIdentities returns the cheap-summary totals envelope for
// the scoped identity slice.
func (s PostgresContainerImageIdentityAggregateStore) CountContainerImageIdentities(
	ctx context.Context,
	filter ContainerImageIdentityAggregateFilter,
) (ContainerImageIdentityAggregateCount, error) {
	if s.DB == nil {
		return ContainerImageIdentityAggregateCount{}, fmt.Errorf("container image identity aggregate database is required")
	}

	args := []any{
		filter.Digest,
		filter.ImageRef,
		filter.SourceRepositoryID,
		filter.RepositoryID,
		filter.Outcome,
		pq.Array(filter.AllowedSourceRepositoryIDs),
	}

	row := s.DB.QueryRowContext(ctx, containerImageIdentityAggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return ContainerImageIdentityAggregateCount{}, fmt.Errorf("count container image identities: %w", err)
	}

	out := ContainerImageIdentityAggregateCount{
		TotalIdentities:    int(total.Int64),
		ByOutcome:          map[string]int{},
		ByIdentityStrength: map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'outcome', ''), 'unknown')", out.ByOutcome); err != nil {
		return ContainerImageIdentityAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'identity_strength', ''), 'unknown')", out.ByIdentityStrength); err != nil {
		return ContainerImageIdentityAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresContainerImageIdentityAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(containerImageIdentityAggregateGroupQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("group container image identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan container image identity bucket: %w", err)
		}
		dst[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// ContainerImageIdentityInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s PostgresContainerImageIdentityAggregateStore) ContainerImageIdentityInventory(
	ctx context.Context,
	filter ContainerImageIdentityAggregateFilter,
	dimension ContainerImageIdentityInventoryDimension,
	limit int,
	offset int,
) ([]ContainerImageIdentityInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("container image identity aggregate database is required")
	}
	groupExpr, err := containerImageIdentityInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > ContainerImageIdentityAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", ContainerImageIdentityAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(containerImageIdentityInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.Digest,
		filter.ImageRef,
		filter.SourceRepositoryID,
		filter.RepositoryID,
		filter.Outcome,
		pq.Array(filter.AllowedSourceRepositoryIDs),
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory container image identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]ContainerImageIdentityInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan container image identity inventory row: %w", err)
		}
		out = append(out, ContainerImageIdentityInventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate container image identity inventory rows: %w", err)
	}
	return out, nil
}

// containerImageIdentityInventoryGroupExpression maps the dimension enum to
// the safe SQL expression substituted into the inventory query template. Only
// known enum values are accepted, so the substitution stays parameter-safe;
// filter values flow through bound parameters only.
func containerImageIdentityInventoryGroupExpression(
	dimension ContainerImageIdentityInventoryDimension,
) (string, error) {
	switch dimension {
	case ContainerImageIdentityInventoryByOutcome:
		return "COALESCE(NULLIF(fact.payload->>'outcome', ''), 'unknown')", nil
	case ContainerImageIdentityInventoryByIdentityStrength:
		return "COALESCE(NULLIF(fact.payload->>'identity_strength', ''), 'unknown')", nil
	case ContainerImageIdentityInventoryByRepository:
		return "COALESCE(NULLIF(fact.payload->>'repository_id', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported container image identity inventory dimension: %q", dimension)
	}
}
