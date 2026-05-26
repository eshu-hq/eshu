package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DocumentationFindingAggregateStore reads cheap-summary aggregates over
// reducer-owned documentation findings. It replaces the page-and-iterate
// caller workflow for ecosystem-level questions like "how many findings per
// status?" or "which sources have the most authoritative documentation?"
// exposed by list_documentation_findings.
//
// All reads inherit the same permission predicates the list endpoint uses
// (`viewer_can_read_source`, `source_acl_evaluated`,
// `permission_decision`) so an aggregate cannot leak counts from documents
// the caller is not allowed to read. The constants are mirrored verbatim
// from `buildDocumentationFindingsSQL` (documentation_read_model.go) and
// from the partial index in
// `go/internal/storage/postgres/schema_fact_records.go`.
type DocumentationFindingAggregateStore interface {
	CountDocumentationFindings(context.Context, DocumentationFindingAggregateFilter) (DocumentationFindingAggregateCount, error)
	DocumentationFindingInventory(
		context.Context,
		DocumentationFindingAggregateFilter,
		DocumentationFindingInventoryDimension,
		int,
		int,
	) ([]DocumentationFindingInventoryRow, error)
}

// DocumentationFindingInventoryDimension names the grouping dimension for
// the inventory aggregate.
type DocumentationFindingInventoryDimension string

const (
	// DocumentationFindingInventoryByStatus groups by reducer status.
	DocumentationFindingInventoryByStatus DocumentationFindingInventoryDimension = "status"
	// DocumentationFindingInventoryByTruthLevel groups by reducer
	// truth_level.
	DocumentationFindingInventoryByTruthLevel DocumentationFindingInventoryDimension = "truth_level"
	// DocumentationFindingInventoryByFreshnessState groups by reducer
	// freshness_state.
	DocumentationFindingInventoryByFreshnessState DocumentationFindingInventoryDimension = "freshness_state"
	// DocumentationFindingInventoryByFindingType groups by finding_type.
	DocumentationFindingInventoryByFindingType DocumentationFindingInventoryDimension = "finding_type"
	// DocumentationFindingInventoryBySourceID groups by upstream source_id
	// (Confluence space, Notion workspace, etc.).
	DocumentationFindingInventoryBySourceID DocumentationFindingInventoryDimension = "source_id"
)

// DocumentationFindingAggregateMaxLimit caps inventory result pages.
const DocumentationFindingAggregateMaxLimit = 500

// DocumentationFindingAggregateFilter narrows aggregate reads. An aggregate
// without a scope is allowed because the dataset is already bounded to
// `fact_kind = 'documentation_finding'` plus the permission predicates at
// index lookup time.
type DocumentationFindingAggregateFilter struct {
	ScopeID        string
	FindingType    string
	SourceID       string
	DocumentID     string
	Status         string
	TruthLevel     string
	FreshnessState string
}

// DocumentationFindingAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByStatus / ByTruthLevel / ByFreshnessState are
// pre-aggregated rollups so callers can answer the three most common
// per-dimension questions without a second round trip.
type DocumentationFindingAggregateCount struct {
	TotalFindings    int
	ByStatus         map[string]int
	ByTruthLevel     map[string]int
	ByFreshnessState map[string]int
}

// DocumentationFindingInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type DocumentationFindingInventoryRow struct {
	Dimension DocumentationFindingInventoryDimension `json:"dimension"`
	Value     string                                 `json:"value"`
	Count     int                                    `json:"count"`
}

// PostgresDocumentationFindingAggregateStore reads aggregate counts directly
// from reducer-owned documentation findings facts.
type PostgresDocumentationFindingAggregateStore struct {
	DB documentationFindingAggregateQueryer
}

type documentationFindingAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresDocumentationFindingAggregateStore creates the Postgres-backed
// aggregate store.
func NewPostgresDocumentationFindingAggregateStore(
	db documentationFindingAggregateQueryer,
) PostgresDocumentationFindingAggregateStore {
	return PostgresDocumentationFindingAggregateStore{DB: db}
}

// The permission predicates are constants (not optional) because they gate
// the entire partial index in schema_fact_records.go. Skipping them would
// (a) miss the index entirely and (b) leak counts from documents the caller
// cannot read.
const documentationFindingAggregatePermissionFilter = `
  AND fact_records.fact_kind = 'documentation_finding'
  AND fact_records.is_tombstone = FALSE
  AND (fact_records.payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(fact_records.payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(fact_records.payload->'states'->>'permission_decision', '')) <> 'denied'
`

const documentationFindingAggregateOptionalFilters = `
  AND ($1 = '' OR fact_records.scope_id = $1)
  AND ($2 = '' OR fact_records.payload->>'finding_type' = $2)
  AND ($3 = '' OR fact_records.payload->>'source_id' = $3)
  AND ($4 = '' OR fact_records.payload->>'document_id' = $4)
  AND ($5 = '' OR fact_records.payload->>'status' = $5)
  AND ($6 = '' OR fact_records.payload->>'truth_level' = $6)
  AND ($7 = '' OR fact_records.payload->>'freshness_state' = $7)
`

var documentationFindingAggregateTotalQuery = `
SELECT COUNT(*) AS total
FROM fact_records
WHERE TRUE` + documentationFindingAggregatePermissionFilter + documentationFindingAggregateOptionalFilters + ";"

var documentationFindingAggregateGroupQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records
WHERE TRUE` + documentationFindingAggregatePermissionFilter + documentationFindingAggregateOptionalFilters + `
GROUP BY bucket;
`

var documentationFindingInventoryQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records
WHERE TRUE` + documentationFindingAggregatePermissionFilter + documentationFindingAggregateOptionalFilters + `
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $8 OFFSET $9;
`

// CountDocumentationFindings returns the cheap-summary totals envelope for
// the scoped findings slice.
func (s PostgresDocumentationFindingAggregateStore) CountDocumentationFindings(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateCount, error) {
	if s.DB == nil {
		return DocumentationFindingAggregateCount{}, fmt.Errorf("documentation finding aggregate database is required")
	}

	args := []any{
		filter.ScopeID,
		filter.FindingType,
		filter.SourceID,
		filter.DocumentID,
		filter.Status,
		filter.TruthLevel,
		filter.FreshnessState,
	}

	row := s.DB.QueryRowContext(ctx, documentationFindingAggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return DocumentationFindingAggregateCount{}, fmt.Errorf("count documentation findings: %w", err)
	}

	out := DocumentationFindingAggregateCount{
		TotalFindings:    int(total.Int64),
		ByStatus:         map[string]int{},
		ByTruthLevel:     map[string]int{},
		ByFreshnessState: map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact_records.payload->>'status', ''), 'unknown')", out.ByStatus); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact_records.payload->>'truth_level', ''), 'unknown')", out.ByTruthLevel); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact_records.payload->>'freshness_state', ''), 'unknown')", out.ByFreshnessState); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresDocumentationFindingAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(documentationFindingAggregateGroupQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("group documentation findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan documentation finding bucket: %w", err)
		}
		dst[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// DocumentationFindingInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller.
func (s PostgresDocumentationFindingAggregateStore) DocumentationFindingInventory(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
	dimension DocumentationFindingInventoryDimension,
	limit int,
	offset int,
) ([]DocumentationFindingInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("documentation finding aggregate database is required")
	}
	groupExpr, err := documentationFindingInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > DocumentationFindingAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", DocumentationFindingAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(documentationFindingInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.ScopeID,
		filter.FindingType,
		filter.SourceID,
		filter.DocumentID,
		filter.Status,
		filter.TruthLevel,
		filter.FreshnessState,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory documentation findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]DocumentationFindingInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan documentation finding inventory row: %w", err)
		}
		out = append(out, DocumentationFindingInventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documentation finding inventory rows: %w", err)
	}
	return out, nil
}

// documentationFindingInventoryGroupExpression maps the dimension enum to
// the safe SQL expression substituted into the inventory query template.
// Only known enum values are accepted, so the substitution stays
// parameter-safe; filter values flow through bound parameters only.
func documentationFindingInventoryGroupExpression(
	dimension DocumentationFindingInventoryDimension,
) (string, error) {
	switch dimension {
	case DocumentationFindingInventoryByStatus:
		return "COALESCE(NULLIF(fact_records.payload->>'status', ''), 'unknown')", nil
	case DocumentationFindingInventoryByTruthLevel:
		return "COALESCE(NULLIF(fact_records.payload->>'truth_level', ''), 'unknown')", nil
	case DocumentationFindingInventoryByFreshnessState:
		return "COALESCE(NULLIF(fact_records.payload->>'freshness_state', ''), 'unknown')", nil
	case DocumentationFindingInventoryByFindingType:
		return "COALESCE(NULLIF(fact_records.payload->>'finding_type', ''), 'unknown')", nil
	case DocumentationFindingInventoryBySourceID:
		return "COALESCE(NULLIF(fact_records.payload->>'source_id', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported documentation finding inventory dimension: %q", dimension)
	}
}
