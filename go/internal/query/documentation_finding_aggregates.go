// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
// index lookup time. AllowedRepositoryIDs and AllowedScopeIDs are injected by
// the handler from AuthContext for scoped tokens and must be applied before
// grouping, ordering, and paging.
type DocumentationFindingAggregateFilter struct {
	ScopeID              string
	FindingType          string
	SourceID             string
	DocumentID           string
	Status               string
	TruthLevel           string
	FreshnessState       string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
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

// CountDocumentationFindings returns the cheap-summary totals envelope for
// the scoped findings slice.
func (s PostgresDocumentationFindingAggregateStore) CountDocumentationFindings(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateCount, error) {
	if s.DB == nil {
		return DocumentationFindingAggregateCount{}, fmt.Errorf("documentation finding aggregate database is required")
	}

	q, args := buildDocumentationFindingAggregateTotalSQL(filter)
	row := s.DB.QueryRowContext(ctx, q, args...)
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
	if err := s.fillBuckets(ctx, filter, "COALESCE(NULLIF(fact_records.payload->>'status', ''), 'unknown')", out.ByStatus); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, filter, "COALESCE(NULLIF(fact_records.payload->>'truth_level', ''), 'unknown')", out.ByTruthLevel); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, filter, "COALESCE(NULLIF(fact_records.payload->>'freshness_state', ''), 'unknown')", out.ByFreshnessState); err != nil {
		return DocumentationFindingAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresDocumentationFindingAggregateStore) fillBuckets(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
	groupExpr string,
	dst map[string]int,
) error {
	q, args := buildDocumentationFindingAggregateGroupSQL(filter, groupExpr)
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
	q, args := buildDocumentationFindingInventorySQL(filter, groupExpr, limit, offset)
	rows, err := s.DB.QueryContext(ctx, q, args...)
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

func buildDocumentationFindingAggregateTotalSQL(filter DocumentationFindingAggregateFilter) (string, []any) {
	clauses, args := documentationFindingAggregateClauses(filter)
	return fmt.Sprintf(`
SELECT COUNT(*) AS total
FROM fact_records%s
WHERE %s;
`, documentationFindingAggregateScopeJoin(filter), strings.Join(clauses, " AND ")), args
}

func buildDocumentationFindingAggregateGroupSQL(
	filter DocumentationFindingAggregateFilter,
	groupExpr string,
) (string, []any) {
	clauses, args := documentationFindingAggregateClauses(filter)
	return fmt.Sprintf(`
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records%s
WHERE %s
GROUP BY bucket;
`, groupExpr, documentationFindingAggregateScopeJoin(filter), strings.Join(clauses, " AND ")), args
}

func buildDocumentationFindingInventorySQL(
	filter DocumentationFindingAggregateFilter,
	groupExpr string,
	limit int,
	offset int,
) (string, []any) {
	clauses, args := documentationFindingAggregateClauses(filter)
	args = append(args, limit, offset)
	return fmt.Sprintf(`
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM fact_records%s
WHERE %s
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $%d OFFSET $%d;
`, groupExpr, documentationFindingAggregateScopeJoin(filter), strings.Join(clauses, " AND "), len(args)-1, len(args)), args
}

func documentationFindingAggregateClauses(filter DocumentationFindingAggregateFilter) ([]string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.fact_kind = 'documentation_finding'",
		"fact_records.is_tombstone = FALSE",
		"(fact_records.payload->'permissions'->>'viewer_can_read_source') = 'true'",
		"LOWER(COALESCE(fact_records.payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'",
		"LOWER(COALESCE(fact_records.payload->'states'->>'permission_decision', '')) <> 'denied'",
	}
	addColumnFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", field, len(args)))
	}
	addPayloadFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	addColumnFilter("fact_records.scope_id", filter.ScopeID)
	addPayloadFilter("finding_type", filter.FindingType)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	addPayloadFilter("status", filter.Status)
	addPayloadFilter("truth_level", filter.TruthLevel)
	addPayloadFilter("freshness_state", filter.FreshnessState)
	return appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
}

func documentationFindingAggregateScopeJoin(filter DocumentationFindingAggregateFilter) string {
	if documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs) {
		return "\nLEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id"
	}
	return ""
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
