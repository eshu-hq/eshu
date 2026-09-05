// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

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
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
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
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(canonical.outcome, ''), 'unknown')", out.ByOutcome); err != nil {
		return ContainerImageIdentityAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(canonical.canonical_identity_strength, ''), 'unknown')", out.ByIdentityStrength); err != nil {
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
	q := containerImageIdentityInventoryQueryTemplate
	if dimension != ContainerImageIdentityInventoryByRepository {
		q = fmt.Sprintf(containerImageIdentityCanonicalInventoryQueryTemplate, groupExpr)
	}
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.Digest,
		filter.ImageRef,
		filter.SourceRepositoryID,
		filter.RepositoryID,
		filter.Outcome,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
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
		return "COALESCE(NULLIF(canonical.outcome, ''), 'unknown')", nil
	case ContainerImageIdentityInventoryByIdentityStrength:
		return "COALESCE(NULLIF(canonical.canonical_identity_strength, ''), 'unknown')", nil
	case ContainerImageIdentityInventoryByRepository:
		return "COALESCE(NULLIF(support.repository_id, ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported container image identity inventory dimension: %q", dimension)
	}
}
