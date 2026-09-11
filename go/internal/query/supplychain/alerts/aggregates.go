// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// PostgresAggregateStore reads aggregate counts directly from reducer-owned
// reconciliation facts.
type PostgresAggregateStore struct {
	DB AggregateQueryer
}

// AggregateQueryer is the narrow *sql.DB surface PostgresAggregateStore
// needs. Exported because it is NewPostgresAggregateStore's parameter type:
// root's NewPostgresSecurityAlertReconciliationAggregateStore forwarder in
// supply_chain_hub_alias.go, and in turn cmd/api and cmd/mcp-server wiring
// (which pass a *sql.DB), must be able to name it.
type AggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresAggregateStore creates the Postgres-backed aggregate store.
func NewPostgresAggregateStore(db AggregateQueryer) PostgresAggregateStore {
	return PostgresAggregateStore{DB: db}
}

// PostgresAggregateStore satisfies the hub's aggregate read port; drift
// fails here rather than at a call site. Moved from root's
// supply_chain_hub_alias.go pin (#6642).
var _ supplychain.SecurityAlertReconciliationAggregateStore = PostgresAggregateStore{}

// SecurityAlertProviderRepositoryScopes returns provider-owned security alert
// repository scopes whose exact repository-name segment matches the supplied
// source repository name. It shares the list store's lookup so aggregate and
// list routes resolve repository-scoped provider-only alerts identically.
// The method name matches the sibling list-store contract; it is not stutter.
func (s PostgresAggregateStore) SecurityAlertProviderRepositoryScopes(
	ctx context.Context,
	repositoryName string,
) ([]string, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation aggregate database is required")
	}
	return providerRepositoryScopes(ctx, s.DB, repositoryName)
}

// CountSecurityAlertReconciliations returns the cheap-summary totals envelope
// for the scoped reconciliation slice. The method name matches
// supplychain.SecurityAlertReconciliationAggregateStore; it is a port
// contract, not stutter.
func (s PostgresAggregateStore) CountSecurityAlertReconciliations(
	ctx context.Context,
	filter supplychain.SecurityAlertReconciliationAggregateFilter,
) (supplychain.SecurityAlertReconciliationAggregateCount, error) {
	if s.DB == nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, fmt.Errorf("security alert reconciliation aggregate database is required")
	}

	args := []any{
		pgarray.Array(supplychain.SecurityAlertRepositoryScopeIDs(filter.RepositoryID, filter.RepositoryScopeIDs)),
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
	}

	row := s.DB.QueryRowContext(ctx, aggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, fmt.Errorf("count security alert reconciliations: %w", err)
	}

	out := supplychain.SecurityAlertReconciliationAggregateCount{
		TotalReconciliations:   int(total.Int64),
		ByReconciliationStatus: map[string]int{},
		ByProvider:             map[string]int{},
		ByProviderState:        map[string]int{},
		BySourceFreshness:      map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(current_fact.payload->>'reconciliation_status', ''), 'unknown')", out.ByReconciliationStatus); err != nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(current_fact.payload->>'provider', ''), 'unknown')", out.ByProvider); err != nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(current_fact.payload->>'provider_state', ''), 'unknown')", out.ByProviderState); err != nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, sourceFreshnessGroupExpr, out.BySourceFreshness); err != nil {
		return supplychain.SecurityAlertReconciliationAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(aggregateGroupQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("group security alert reconciliations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan security alert reconciliation bucket: %w", err)
		}
		dst[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// SecurityAlertReconciliationInventory returns a paginated grouped count along
// the requested dimension. Limit and offset must already be normalized by the
// caller. The method name matches
// supplychain.SecurityAlertReconciliationAggregateStore; it is a port
// contract, not stutter.
func (s PostgresAggregateStore) SecurityAlertReconciliationInventory(
	ctx context.Context,
	filter supplychain.SecurityAlertReconciliationAggregateFilter,
	dimension supplychain.SecurityAlertReconciliationInventoryDimension,
	limit int,
	offset int,
) ([]supplychain.SecurityAlertReconciliationInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation aggregate database is required")
	}
	groupExpr, err := inventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > supplychain.SecurityAlertReconciliationAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", supplychain.SecurityAlertReconciliationAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(inventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		pgarray.Array(supplychain.SecurityAlertRepositoryScopeIDs(filter.RepositoryID, filter.RepositoryScopeIDs)),
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory security alert reconciliations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]supplychain.SecurityAlertReconciliationInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan security alert reconciliation inventory row: %w", err)
		}
		out = append(out, supplychain.SecurityAlertReconciliationInventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security alert reconciliation inventory rows: %w", err)
	}
	return out, nil
}

// inventoryGroupExpression maps the dimension enum to the safe SQL expression
// substituted into the inventory and group-by aggregate query templates.
// Only known enum values are accepted, so the substitution stays
// parameter-safe; filter values flow through bound parameters only.
//
// TestSecurityAlertReconciliationInventoryGroupExpressionEnumIsClosed
// (aggregates_test.go) pins this enum-closed contract in-package.
func inventoryGroupExpression(
	dimension supplychain.SecurityAlertReconciliationInventoryDimension,
) (string, error) {
	switch dimension {
	case supplychain.SecurityAlertReconciliationInventoryByStatus:
		return "COALESCE(NULLIF(current_fact.payload->>'reconciliation_status', ''), 'unknown')", nil
	case supplychain.SecurityAlertReconciliationInventoryByProvider:
		return "COALESCE(NULLIF(current_fact.payload->>'provider', ''), 'unknown')", nil
	case supplychain.SecurityAlertReconciliationInventoryByProviderState:
		return "COALESCE(NULLIF(current_fact.payload->>'provider_state', ''), 'unknown')", nil
	case supplychain.SecurityAlertReconciliationInventoryByRepository:
		return "COALESCE(NULLIF(current_fact.payload->>'repository_id', ''), 'unknown')", nil
	case supplychain.SecurityAlertReconciliationInventoryByPackage:
		return "COALESCE(NULLIF(current_fact.payload->>'package_id', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported security alert reconciliation inventory dimension: %q", dimension)
	}
}
