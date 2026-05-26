package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SecurityAlertReconciliationAggregateStore reads cheap-summary aggregates
// over reducer-owned provider security alert reconciliations. It replaces the
// page-and-iterate caller workflow for ecosystem-level questions like "how
// many alerts are in `eshu_only` reconciliation status across all repos?".
type SecurityAlertReconciliationAggregateStore interface {
	CountSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationAggregateFilter) (SecurityAlertReconciliationAggregateCount, error)
	SecurityAlertReconciliationInventory(
		context.Context,
		SecurityAlertReconciliationAggregateFilter,
		SecurityAlertReconciliationInventoryDimension,
		int,
		int,
	) ([]SecurityAlertReconciliationInventoryRow, error)
}

// SecurityAlertReconciliationInventoryDimension names the grouping dimension
// for the inventory aggregate.
type SecurityAlertReconciliationInventoryDimension string

const (
	// SecurityAlertReconciliationInventoryByStatus groups by reducer
	// reconciliation_status.
	SecurityAlertReconciliationInventoryByStatus SecurityAlertReconciliationInventoryDimension = "reconciliation_status"
	// SecurityAlertReconciliationInventoryByProvider groups by provider.
	SecurityAlertReconciliationInventoryByProvider SecurityAlertReconciliationInventoryDimension = "provider"
	// SecurityAlertReconciliationInventoryByProviderState groups by provider
	// state (open, fixed, dismissed, etc.).
	SecurityAlertReconciliationInventoryByProviderState SecurityAlertReconciliationInventoryDimension = "provider_state"
	// SecurityAlertReconciliationInventoryByRepository groups by repository_id.
	SecurityAlertReconciliationInventoryByRepository SecurityAlertReconciliationInventoryDimension = "repository_id"
	// SecurityAlertReconciliationInventoryByPackage groups by package_id.
	SecurityAlertReconciliationInventoryByPackage SecurityAlertReconciliationInventoryDimension = "package_id"
)

// SecurityAlertReconciliationAggregateMaxLimit caps inventory result pages.
const SecurityAlertReconciliationAggregateMaxLimit = 500

// SecurityAlertReconciliationAggregateFilter narrows aggregate reads. An
// aggregate without a scope is allowed because the totals question itself is
// the call shape we want to support — the dataset is already bounded by
// `fact_kind` and the active-generation predicate at index lookup time.
type SecurityAlertReconciliationAggregateFilter struct {
	RepositoryID         string
	Provider             string
	PackageID            string
	CVEID                string
	GHSAID               string
	ProviderState        string
	ReconciliationStatus string
}

// SecurityAlertReconciliationAggregateCount is the cheap-summary totals
// envelope used by the count handler. ByReconciliationStatus and ByProvider
// are pre-aggregated rollups so callers can answer "alerts per provider" and
// "alerts per reconciliation status" without a second round trip.
type SecurityAlertReconciliationAggregateCount struct {
	TotalReconciliations   int
	ByReconciliationStatus map[string]int
	ByProvider             map[string]int
	ByProviderState        map[string]int
}

// SecurityAlertReconciliationInventoryRow is one grouped bucket returned by
// the inventory aggregate.
type SecurityAlertReconciliationInventoryRow struct {
	Dimension SecurityAlertReconciliationInventoryDimension `json:"dimension"`
	Value     string                                        `json:"value"`
	Count     int                                           `json:"count"`
}

// PostgresSecurityAlertReconciliationAggregateStore reads aggregate counts
// directly from reducer-owned reconciliation facts.
type PostgresSecurityAlertReconciliationAggregateStore struct {
	DB securityAlertReconciliationAggregateQueryer
}

type securityAlertReconciliationAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresSecurityAlertReconciliationAggregateStore creates the
// Postgres-backed aggregate store.
func NewPostgresSecurityAlertReconciliationAggregateStore(
	db securityAlertReconciliationAggregateQueryer,
) PostgresSecurityAlertReconciliationAggregateStore {
	return PostgresSecurityAlertReconciliationAggregateStore{DB: db}
}

const securityAlertReconciliationAggregateTotalQuery = `
SELECT COUNT(*) AS total
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_security_alert_reconciliation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'repository_id' = $1)
  AND ($2 = '' OR fact.payload->>'provider' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->'cve_ids' ? $4)
  AND ($5 = '' OR fact.payload->'ghsa_ids' ? $5)
  AND ($6 = '' OR fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR fact.payload->>'reconciliation_status' = $7);
`

const securityAlertReconciliationAggregateGroupQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_security_alert_reconciliation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'repository_id' = $1)
  AND ($2 = '' OR fact.payload->>'provider' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->'cve_ids' ? $4)
  AND ($5 = '' OR fact.payload->'ghsa_ids' ? $5)
  AND ($6 = '' OR fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR fact.payload->>'reconciliation_status' = $7)
GROUP BY bucket;
`

const securityAlertReconciliationInventoryQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_security_alert_reconciliation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($1 = '' OR fact.payload->>'repository_id' = $1)
  AND ($2 = '' OR fact.payload->>'provider' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->'cve_ids' ? $4)
  AND ($5 = '' OR fact.payload->'ghsa_ids' ? $5)
  AND ($6 = '' OR fact.payload->>'provider_state' = $6)
  AND ($7 = '' OR fact.payload->>'reconciliation_status' = $7)
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $8 OFFSET $9;
`

// CountSecurityAlertReconciliations returns the cheap-summary totals envelope
// for the scoped reconciliation slice.
func (s PostgresSecurityAlertReconciliationAggregateStore) CountSecurityAlertReconciliations(
	ctx context.Context,
	filter SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	if s.DB == nil {
		return SecurityAlertReconciliationAggregateCount{}, fmt.Errorf("security alert reconciliation aggregate database is required")
	}

	args := []any{
		filter.RepositoryID,
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
	}

	row := s.DB.QueryRowContext(ctx, securityAlertReconciliationAggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return SecurityAlertReconciliationAggregateCount{}, fmt.Errorf("count security alert reconciliations: %w", err)
	}

	out := SecurityAlertReconciliationAggregateCount{
		TotalReconciliations:   int(total.Int64),
		ByReconciliationStatus: map[string]int{},
		ByProvider:             map[string]int{},
		ByProviderState:        map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'reconciliation_status', ''), 'unknown')", out.ByReconciliationStatus); err != nil {
		return SecurityAlertReconciliationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'provider', ''), 'unknown')", out.ByProvider); err != nil {
		return SecurityAlertReconciliationAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'provider_state', ''), 'unknown')", out.ByProviderState); err != nil {
		return SecurityAlertReconciliationAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresSecurityAlertReconciliationAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(securityAlertReconciliationAggregateGroupQueryTemplate, groupExpr)
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
// caller.
func (s PostgresSecurityAlertReconciliationAggregateStore) SecurityAlertReconciliationInventory(
	ctx context.Context,
	filter SecurityAlertReconciliationAggregateFilter,
	dimension SecurityAlertReconciliationInventoryDimension,
	limit int,
	offset int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation aggregate database is required")
	}
	groupExpr, err := securityAlertReconciliationInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > SecurityAlertReconciliationAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", SecurityAlertReconciliationAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(securityAlertReconciliationInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.RepositoryID,
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory security alert reconciliations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]SecurityAlertReconciliationInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan security alert reconciliation inventory row: %w", err)
		}
		out = append(out, SecurityAlertReconciliationInventoryRow{
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

// securityAlertReconciliationInventoryGroupExpression maps the dimension enum
// to the safe SQL expression substituted into the inventory query template.
// Only known enum values are accepted, so the substitution stays
// parameter-safe; filter values flow through bound parameters only.
func securityAlertReconciliationInventoryGroupExpression(
	dimension SecurityAlertReconciliationInventoryDimension,
) (string, error) {
	switch dimension {
	case SecurityAlertReconciliationInventoryByStatus:
		return "COALESCE(NULLIF(fact.payload->>'reconciliation_status', ''), 'unknown')", nil
	case SecurityAlertReconciliationInventoryByProvider:
		return "COALESCE(NULLIF(fact.payload->>'provider', ''), 'unknown')", nil
	case SecurityAlertReconciliationInventoryByProviderState:
		return "COALESCE(NULLIF(fact.payload->>'provider_state', ''), 'unknown')", nil
	case SecurityAlertReconciliationInventoryByRepository:
		return "COALESCE(NULLIF(fact.payload->>'repository_id', ''), 'unknown')", nil
	case SecurityAlertReconciliationInventoryByPackage:
		return "COALESCE(NULLIF(fact.payload->>'package_id', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported security alert reconciliation inventory dimension: %q", dimension)
	}
}
