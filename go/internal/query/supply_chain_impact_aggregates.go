package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SupplyChainImpactAggregateStore reads cheap-summary aggregates over
// reducer-owned vulnerability impact findings without forcing callers to
// page through the full list endpoint.
type SupplyChainImpactAggregateStore interface {
	CountSupplyChainImpactFindings(context.Context, SupplyChainImpactAggregateFilter) (SupplyChainImpactAggregateCount, error)
	SupplyChainImpactInventory(context.Context, SupplyChainImpactAggregateFilter, SupplyChainImpactInventoryDimension, int, int) ([]SupplyChainImpactInventoryRow, error)
}

// SupplyChainImpactInventoryDimension names the grouping dimension for the
// inventory aggregate.
type SupplyChainImpactInventoryDimension string

const (
	// SupplyChainImpactInventoryByImpactStatus groups by reducer impact_status.
	SupplyChainImpactInventoryByImpactStatus SupplyChainImpactInventoryDimension = "impact_status"
	// SupplyChainImpactInventoryByPriorityBucket groups by reducer priority_bucket.
	SupplyChainImpactInventoryByPriorityBucket SupplyChainImpactInventoryDimension = "priority_bucket"
	// SupplyChainImpactInventoryBySeverity groups by CVSS severity bucket
	// (none / low / medium / high / critical).
	SupplyChainImpactInventoryBySeverity SupplyChainImpactInventoryDimension = "severity"
	// SupplyChainImpactInventoryByRepository groups by repository_id.
	SupplyChainImpactInventoryByRepository SupplyChainImpactInventoryDimension = "repository_id"
)

// SupplyChainImpactAggregateMaxLimit caps inventory result pages.
const SupplyChainImpactAggregateMaxLimit = 500

// SupplyChainImpactAggregateFilter narrows aggregate reads to one repository,
// package, CVE, subject digest, profile, priority, or suppression state. An
// aggregate without a scope is allowed because the totals question itself is
// the call shape we want to replace — the dataset is already bounded by
// `fact_kind` and the active-generation predicate at index lookup time.
// DetectionProfile uses the same downstream value as the list route: `precise`
// narrows to exact installed-version anchors, and blank admits both precise and
// comprehensive rows.
type SupplyChainImpactAggregateFilter struct {
	CVEID             string
	PackageID         string
	RepositoryID      string
	SubjectDigest     string
	ImpactStatus      string
	DetectionProfile  string
	PriorityBucket    string
	MinPriorityScore  int
	SuppressionState  string
	IncludeSuppressed bool
}

// SupplyChainImpactAggregateCount is the cheap-summary totals envelope used by
// the count handler. AffectedExact and AffectedDerived correspond to the
// reducer-owned impact_status values 'affected_exact' and 'affected_derived';
// PossiblyAffected covers 'possibly_affected'. NotAffected counts every
// impact_status value with the 'not_affected' prefix.
type SupplyChainImpactAggregateCount struct {
	TotalFindings    int
	AffectedFindings int
	NotAffected      int
	AffectedExact    int
	AffectedDerived  int
	PossiblyAffected int
	ByPriorityBucket map[string]int
	BySeverity       map[string]int
}

// SupplyChainImpactInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type SupplyChainImpactInventoryRow struct {
	Dimension SupplyChainImpactInventoryDimension `json:"dimension"`
	Value     string                              `json:"value"`
	Count     int                                 `json:"count"`
}

// PostgresSupplyChainImpactAggregateStore reads aggregate counts directly
// from reducer-owned impact findings facts.
type PostgresSupplyChainImpactAggregateStore struct {
	DB supplyChainImpactAggregateQueryer
}

type supplyChainImpactAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresSupplyChainImpactAggregateStore creates the Postgres-backed
// aggregate store.
func NewPostgresSupplyChainImpactAggregateStore(
	db supplyChainImpactAggregateQueryer,
) PostgresSupplyChainImpactAggregateStore {
	return PostgresSupplyChainImpactAggregateStore{DB: db}
}

const supplyChainImpactAggregateCanonicalFactsCTE = `
WITH scoped_facts AS (
	SELECT fact.fact_id,
	       fact.payload,
	       COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
	       ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
	       ` + supplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key
	FROM fact_records AS fact
	JOIN ingestion_scopes AS scope
	  ON scope.scope_id = fact.scope_id
	 AND scope.active_generation_id = fact.generation_id
	JOIN scope_generations AS generation
	  ON generation.scope_id = fact.scope_id
	 AND generation.generation_id = fact.generation_id
	WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
	  AND fact.is_tombstone = FALSE
	  AND generation.status = 'active'
	  AND ($1 = '' OR fact.payload->>'cve_id' = $1)
	  AND ($2 = '' OR fact.payload->>'package_id' = $2)
	  AND ($3 = '' OR fact.payload->>'repository_id' = $3)
	  AND ($4 = '' OR fact.payload->>'subject_digest' = $4)
	  AND ($5 = '' OR fact.payload->>'impact_status' = $5)
	  AND (
	        $6 = ''
	        OR fact.payload->>'detection_profile' = $6
	        OR (
	              $6 = 'comprehensive'
	              AND COALESCE(fact.payload->>'detection_profile', '') = ''
	           )
	        OR (
	              $6 = 'precise'
	              AND COALESCE(fact.payload->>'detection_profile', '') = ''
	              AND fact.payload->>'impact_status' IN (
	                    'affected_exact',
	                    'not_affected_known_fixed'
	                  )
	              AND COALESCE(fact.payload->>'observed_version', '') <> ''
	              AND fact.payload->>'match_reason' IN (
	                    'npm_semver_affected_range',
	                    'npm_semver_known_fixed',
	                    'maven_range_match',
	                    'maven_known_fixed'
	                  )
	           )
	      )
	  AND ($7 = '' OR fact.payload->>'priority_bucket' = $7)
	  AND ($8 = 0 OR COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $8)
	  AND ($9 = '' OR COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') = $9)
	  AND ($10::boolean OR COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') NOT IN ('not_affected','accepted_risk','false_positive','ignored'))
),
ranked_facts AS (
	SELECT *,
	       ROW_NUMBER() OVER (
	         PARTITION BY canonical_key
	         ORDER BY priority_score DESC, has_payload_finding_id DESC, fact_id ASC
	       ) AS canonical_rank
	FROM scoped_facts
),
canonical_facts AS (
	SELECT payload
	FROM ranked_facts
	WHERE canonical_rank = 1
)
`

const supplyChainImpactAggregateCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COUNT(*) AS total,
	SUM(CASE WHEN payload->>'impact_status' IN ('affected_exact', 'affected_derived', 'possibly_affected') THEN 1 ELSE 0 END) AS affected,
	SUM(CASE WHEN payload->>'impact_status' = 'affected_exact' THEN 1 ELSE 0 END) AS affected_exact,
	SUM(CASE WHEN payload->>'impact_status' = 'affected_derived' THEN 1 ELSE 0 END) AS affected_derived,
	SUM(CASE WHEN payload->>'impact_status' = 'possibly_affected' THEN 1 ELSE 0 END) AS possibly_affected,
	SUM(CASE WHEN payload->>'impact_status' LIKE 'not_affected%' THEN 1 ELSE 0 END) AS not_affected
FROM canonical_facts;
`

const supplyChainImpactAggregatePriorityCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COALESCE(NULLIF(fact.payload->>'priority_bucket', ''), 'unknown') AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

const supplyChainImpactAggregateSeverityCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	CASE
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN 'low'
		ELSE 'none'
	END AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

// CountSupplyChainImpactFindings returns the cheap-summary totals envelope
// for the scoped supply-chain impact slice.
func (s PostgresSupplyChainImpactAggregateStore) CountSupplyChainImpactFindings(
	ctx context.Context,
	filter SupplyChainImpactAggregateFilter,
) (SupplyChainImpactAggregateCount, error) {
	if s.DB == nil {
		return SupplyChainImpactAggregateCount{}, fmt.Errorf("supply chain impact aggregate database is required")
	}

	row := s.DB.QueryRowContext(
		ctx,
		supplyChainImpactAggregateCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
	)
	var total, affected, affectedExact, affectedDerived, possiblyAffected, notAffected sql.NullInt64
	if err := row.Scan(&total, &affected, &affectedExact, &affectedDerived, &possiblyAffected, &notAffected); err != nil {
		return SupplyChainImpactAggregateCount{}, fmt.Errorf("count supply chain impact findings: %w", err)
	}

	count := SupplyChainImpactAggregateCount{
		TotalFindings:    int(total.Int64),
		AffectedFindings: int(affected.Int64),
		AffectedExact:    int(affectedExact.Int64),
		AffectedDerived:  int(affectedDerived.Int64),
		PossiblyAffected: int(possiblyAffected.Int64),
		NotAffected:      int(notAffected.Int64),
		ByPriorityBucket: map[string]int{},
		BySeverity:       map[string]int{},
	}

	if err := s.fillPriorityBuckets(ctx, filter, &count); err != nil {
		return SupplyChainImpactAggregateCount{}, err
	}
	if err := s.fillSeverityBuckets(ctx, filter, &count); err != nil {
		return SupplyChainImpactAggregateCount{}, err
	}
	return count, nil
}

func (s PostgresSupplyChainImpactAggregateStore) fillPriorityBuckets(
	ctx context.Context,
	filter SupplyChainImpactAggregateFilter,
	count *SupplyChainImpactAggregateCount,
) error {
	rows, err := s.DB.QueryContext(
		ctx,
		supplyChainImpactAggregatePriorityCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
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

func (s PostgresSupplyChainImpactAggregateStore) fillSeverityBuckets(
	ctx context.Context,
	filter SupplyChainImpactAggregateFilter,
	count *SupplyChainImpactAggregateCount,
) error {
	rows, err := s.DB.QueryContext(
		ctx,
		supplyChainImpactAggregateSeverityCountQuery,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
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

const supplyChainImpactInventoryQueryTemplate = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $11 OFFSET $12;
`

// SupplyChainImpactInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s PostgresSupplyChainImpactAggregateStore) SupplyChainImpactInventory(
	ctx context.Context,
	filter SupplyChainImpactAggregateFilter,
	dimension SupplyChainImpactInventoryDimension,
	limit int,
	offset int,
) ([]SupplyChainImpactInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("supply chain impact aggregate database is required")
	}
	groupExpr, err := supplyChainImpactInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe (mirrors
	// PostgresSupplyChainImpactFindingStore.ListSupplyChainImpactFindings).
	if limit <= 0 || limit > SupplyChainImpactAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", SupplyChainImpactAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(supplyChainImpactInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]SupplyChainImpactInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan supply chain impact inventory row: %w", err)
		}
		out = append(out, SupplyChainImpactInventoryRow{
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

// supplyChainImpactInventoryGroupExpression maps the dimension enum to the
// safe SQL expression substituted into the inventory query template. Only
// known enum values are accepted, so the substitution stays parameter-safe.
func supplyChainImpactInventoryGroupExpression(dimension SupplyChainImpactInventoryDimension) (string, error) {
	switch dimension {
	case SupplyChainImpactInventoryByImpactStatus:
		return "COALESCE(NULLIF(fact.payload->>'impact_status', ''), 'unknown')", nil
	case SupplyChainImpactInventoryByPriorityBucket:
		return "COALESCE(NULLIF(fact.payload->>'priority_bucket', ''), 'unknown')", nil
	case SupplyChainImpactInventoryBySeverity:
		return `CASE
			WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
			WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
			WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
			WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN 'low'
			ELSE 'none'
		END`, nil
	case SupplyChainImpactInventoryByRepository:
		return "COALESCE(NULLIF(fact.payload->>'repository_id', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported supply chain impact inventory dimension: %q", dimension)
	}
}
