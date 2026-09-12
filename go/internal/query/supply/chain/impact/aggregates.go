// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// AggregateStore reads cheap-summary aggregates over
// reducer-owned vulnerability impact findings without forcing callers to
// page through the full list endpoint.
type AggregateStore interface {
	CountSupplyChainImpactFindings(context.Context, AggregateFilter) (AggregateCount, error)
	SupplyChainImpactInventory(context.Context, AggregateFilter, InventoryDimension, int, int) ([]InventoryRow, error)
}

// InventoryDimension names the grouping dimension for the
// inventory aggregate.
type InventoryDimension string

const (
	// InventoryByStatus groups by reducer impact_status.
	InventoryByStatus InventoryDimension = "impact_status"
	// InventoryByPriorityBucket groups by reducer priority_bucket.
	InventoryByPriorityBucket InventoryDimension = "priority_bucket"
	// InventoryBySeverity groups by CVSS severity bucket
	// (none / low / medium / high / critical).
	InventoryBySeverity InventoryDimension = "severity"
	// InventoryByRepository groups by repository_id.
	InventoryByRepository InventoryDimension = "repository_id"
	// InventoryByEcosystem groups by package ecosystem.
	InventoryByEcosystem InventoryDimension = "ecosystem"
)

// AggregateMaxLimit caps inventory result pages.
const AggregateMaxLimit = 500

// AggregateFilter narrows aggregate reads to one repository,
// package, CVE, subject digest, profile, priority, or suppression state. An
// aggregate without a scope is allowed because the totals question itself is
// the call shape we want to replace — the dataset is already bounded by
// `fact_kind` and the active-generation predicate at index lookup time.
// DetectionProfile uses the same downstream value as the list route: `precise`
// narrows to exact installed-version anchors, and blank admits both precise and
// comprehensive rows.
type AggregateFilter struct {
	CVEID             string
	AdvisoryID        string
	PackageID         string
	RepositoryID      string
	SubjectDigest     string
	ImageRef          string
	Status            string
	Ecosystem         string
	WorkloadID        string
	ServiceID         string
	Environment       string
	Severity          string
	DetectionProfile  string
	PriorityBucket    string
	MinPriorityScore  int
	SuppressionState  string
	IncludeSuppressed bool
	// AllowedRepositoryIDs and AllowedScopeIDs carry scoped-token grants.
	// When both are empty the aggregate is unrestricted. When either is
	// populated the canonical-facts CTE intersects impact facts with the
	// granted repository/scope set before counting, grouping, ordering,
	// limits, and offsets so scoped totals and inventory buckets cover only
	// authorized rows.
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// AggregateCount is the cheap-summary totals envelope used by
// the count handler. AffectedExact and AffectedDerived correspond to the
// reducer-owned impact_status values 'affected_exact' and 'affected_derived';
// PossiblyAffected covers 'possibly_affected'. NotAffected counts every
// impact_status value with the 'not_affected' prefix.
type AggregateCount struct {
	TotalFindings    int
	AffectedFindings int
	NotAffected      int
	AffectedExact    int
	AffectedDerived  int
	PossiblyAffected int
	ByPriorityBucket map[string]int
	BySeverity       map[string]int
}

// InventoryRow is one grouped bucket returned by the
// inventory aggregate.
type InventoryRow struct {
	Dimension InventoryDimension `json:"dimension"`
	Value     string             `json:"value"`
	Count     int                `json:"count"`
}

// PostgresAggregateStore reads aggregate counts directly
// from reducer-owned impact findings facts.
type PostgresAggregateStore struct {
	DB AggregateQueryer
	// Now supplies the single UTC clock value shared by every SQL statement
	// in one aggregate call. It defaults to time.Now.
	Now func() time.Time
}

// AggregateQueryer is the minimal Postgres surface PostgresAggregateStore
// needs; *sql.DB satisfies it.
type AggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresAggregateStore creates the Postgres-backed
// aggregate store.
func NewPostgresAggregateStore(
	db AggregateQueryer,
) PostgresAggregateStore {
	return PostgresAggregateStore{DB: db}
}

// CountSupplyChainImpactFindings returns the cheap-summary totals envelope
// for the scoped supply-chain impact slice.
func (s PostgresAggregateStore) CountSupplyChainImpactFindings(
	ctx context.Context,
	filter AggregateFilter,
) (AggregateCount, error) {
	if s.DB == nil {
		return AggregateCount{}, fmt.Errorf("supply chain impact aggregate database is required")
	}

	readAt := SuppressionReadAt(s.Now)
	row := s.DB.QueryRowContext(
		ctx,
		AggregateCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		readAt,
	)
	var total, affected, affectedExact, affectedDerived, possiblyAffected, notAffected sql.NullInt64
	if err := row.Scan(&total, &affected, &affectedExact, &affectedDerived, &possiblyAffected, &notAffected); err != nil {
		return AggregateCount{}, fmt.Errorf("count supply chain impact findings: %w", err)
	}

	count := AggregateCount{
		TotalFindings:    int(total.Int64),
		AffectedFindings: int(affected.Int64),
		AffectedExact:    int(affectedExact.Int64),
		AffectedDerived:  int(affectedDerived.Int64),
		PossiblyAffected: int(possiblyAffected.Int64),
		NotAffected:      int(notAffected.Int64),
		ByPriorityBucket: map[string]int{},
		BySeverity:       map[string]int{},
	}

	if err := s.fillPriorityBuckets(ctx, filter, readAt, &count); err != nil {
		return AggregateCount{}, err
	}
	if err := s.fillSeverityBuckets(ctx, filter, readAt, &count); err != nil {
		return AggregateCount{}, err
	}
	return count, nil
}

func (s PostgresAggregateStore) fillPriorityBuckets(
	ctx context.Context,
	filter AggregateFilter,
	readAt time.Time,
	count *AggregateCount,
) error {
	rows, err := s.DB.QueryContext(
		ctx,
		AggregatePriorityCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		readAt,
	)
	if err != nil {
		return fmt.Errorf("count supply chain impact priority buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan supply chain impact priority bucket: %w", err)
		}
		count.ByPriorityBucket[bucket] = int(bucketCount)
	}
	return rows.Err()
}

func (s PostgresAggregateStore) fillSeverityBuckets(
	ctx context.Context,
	filter AggregateFilter,
	readAt time.Time,
	count *AggregateCount,
) error {
	rows, err := s.DB.QueryContext(
		ctx,
		AggregateSeverityCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		readAt,
	)
	if err != nil {
		return fmt.Errorf("count supply chain impact severity buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan supply chain impact severity bucket: %w", err)
		}
		count.BySeverity[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// SupplyChainImpactInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s PostgresAggregateStore) SupplyChainImpactInventory(
	ctx context.Context,
	filter AggregateFilter,
	dimension InventoryDimension,
	limit int,
	offset int,
) ([]InventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("supply chain impact aggregate database is required")
	}
	groupExpr, err := supplyChainImpactInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe (mirrors
	// PostgresFindingStore.ListSupplyChainImpactFindings).
	if limit <= 0 || limit > AggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", AggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := InventoryQuery(groupExpr)
	readAt := SuppressionReadAt(s.Now)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		readAt,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]InventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan supply chain impact inventory row: %w", err)
		}
		out = append(out, InventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supply chain impact inventory rows: %w", err)
	}
	return out, nil
}

// InventoryQuery inserts one expression selected by the
// closed InventoryDimension enum. A sentinel replacement is
// deliberate: fmt.Sprintf over the complete SQL would reinterpret percent
// literals in runtime repository-scope LIKE patterns.
func InventoryQuery(groupExpr string) string {
	return strings.Replace(
		InventoryQueryTemplate,
		supplyChainImpactInventoryGroupExpressionPlaceholder,
		groupExpr,
		1,
	)
}

// supplyChainImpactInventoryGroupExpression maps the dimension enum to the
// safe SQL expression substituted into the inventory query template. Only
// known enum values are accepted, so the substitution stays parameter-safe.
func supplyChainImpactInventoryGroupExpression(dimension InventoryDimension) (string, error) {
	switch dimension {
	case InventoryByStatus:
		return "COALESCE(NULLIF(fact.impact_status, ''), 'unknown')", nil
	case InventoryByPriorityBucket:
		return "COALESCE(NULLIF(fact.priority_bucket, ''), 'unknown')", nil
	case InventoryBySeverity:
		return "COALESCE(NULLIF(fact.severity_bucket, ''), 'none')", nil
	case InventoryByRepository:
		return "COALESCE(NULLIF(fact.repository_id, ''), 'unknown')", nil
	case InventoryByEcosystem:
		return "COALESCE(NULLIF(fact.ecosystem, ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported supply chain impact inventory dimension: %q", dimension)
	}
}
