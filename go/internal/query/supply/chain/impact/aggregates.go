// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// AggregateStore reads cheap-summary aggregates over
// reducer-owned vulnerability impact findings without forcing callers to
// page through the full list endpoint.
type AggregateStore interface {
	CountSupplyChainImpactFindings(context.Context, AggregateFilter) (AggregateCount, error)
	SupplyChainImpactInventory(context.Context, AggregateFilter, InventoryDimension, int, int) (AggregateInventoryPage, error)
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
// impact_status value with the 'not_affected' prefix. Freshness reports the
// evidence behind the totals: the legacy live-facts read is always current,
// while a winners-served envelope carries the maintainer watermark the
// handler discloses.
type AggregateCount struct {
	TotalFindings    int
	AffectedFindings int
	NotAffected      int
	AffectedExact    int
	AffectedDerived  int
	PossiblyAffected int
	ByPriorityBucket map[string]int
	BySeverity       map[string]int
	Freshness        AggregateFreshness
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
	DB     AggregateQueryer
	reader aggregateReadQueryer
	// Now supplies the single UTC clock value shared by every SQL statement
	// in one aggregate call. It defaults to time.Now.
	Now func() time.Time
	// Logger receives the WARN line of a failed winners watermark probe. A
	// nil logger drops it; the call still answers
	// ErrAggregateReadModelUnavailable, mirroring the grouped inventory
	// store.
	Logger *slog.Logger
	// ReadFromWinners switches the count and inventory reads to the
	// maintained supply_chain_impact_canonical_winners read model (#10880
	// slice 2b). When false (the default) the reads deduplicate at query
	// time with the legacy canonical-facts CTE. The wiring sets this from
	// an operator env gate so the cutover is reversible and is only
	// enabled once the reducer maintainer has populated the winners
	// table. Output is byte-identical either way on the parity corpus.
	// Scoped-token calls are answered from the per-support table
	// (supply_chain_impact_finding_supports, #10880 PR-B2a), mirroring the
	// list read, because the winners table picks one winner per canonical
	// key before the grant applies while the supports read applies the grant
	// before the pick. A supports table that is not ready answers
	// ErrAggregateReadModelUnavailable, never the winners or legacy read.
	ReadFromWinners bool
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

// NewPostgresAggregateStoreWithReadModel creates the store with
// the #10880 slice 2b read gate set. readFromWinners=true serves the count
// and inventory from the maintained supply_chain_impact_canonical_winners
// read model; false keeps the legacy read-time dedup. The wiring resolves
// readFromWinners from an operator env gate.
func NewPostgresAggregateStoreWithReadModel(
	db AggregateQueryer,
	readFromWinners bool,
) PostgresAggregateStore {
	return PostgresAggregateStore{DB: db, ReadFromWinners: readFromWinners}
}

// NewPostgresAggregateStoreWithReadStore routes count and inventory queries
// through a guarded read-only port while preserving the selected winners
// read model.
func NewPostgresAggregateStoreWithReadStore(reader db.ReadStore, readFromWinners bool) PostgresAggregateStore {
	return PostgresAggregateStore{reader: reader, ReadFromWinners: readFromWinners}
}

// readFromWinnersFor reports whether one aggregate call serves from the
// winners read model. Scoped-token calls and existence-filter calls
// (image_ref, profile) stay on the legacy CTE even when the gate is on,
// mirroring PostgresFindingStore: the winners row is one pick across scopes and
// holds one support's image ref and profile.
func (s PostgresAggregateStore) readFromWinnersFor(filter AggregateFilter) bool {
	return s.ReadFromWinners &&
		len(filter.AllowedRepositoryIDs) == 0 &&
		len(filter.AllowedScopeIDs) == 0 &&
		filter.ImageRef == "" &&
		filter.DetectionProfile == ""
}

type aggregateReadQueryer interface {
	db.Queryer
	db.RowQueryer
}

type legacyAggregateQueryer struct{ database AggregateQueryer }

func (q legacyAggregateQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.database.QueryContext(ctx, query, args...)
}

func (q legacyAggregateQueryer) QueryRowContext(ctx context.Context, query string, args ...any) db.Row {
	return q.database.QueryRowContext(ctx, query, args...)
}

func (s PostgresAggregateStore) queryer() aggregateReadQueryer {
	if s.reader != nil {
		return s.reader
	}
	return legacyAggregateQueryer{database: s.DB}
}

// CountSupplyChainImpactFindings returns the cheap-summary totals envelope
// for the scoped supply-chain impact slice in one Postgres statement
// (AggregateCombinedCountQuery, #10970): totals plus priority and severity
// buckets share a single canonical-facts CTE build instead of the three
// sequential full-CTE statements the route paid before.
func (s PostgresAggregateStore) CountSupplyChainImpactFindings(
	ctx context.Context,
	filter AggregateFilter,
) (AggregateCount, error) {
	if s.DB == nil && s.reader == nil {
		return AggregateCount{}, fmt.Errorf("supply chain impact aggregate database is required")
	}

	readAt := SuppressionReadAt(s.Now)
	// A winners-eligible call probes the maintainer watermark first and
	// fails closed when the model cannot serve an exact answer; the legacy
	// path reports plain freshness and issues no probe.
	freshness, serveWinners, err := s.resolveAggregateFreshness(ctx, filter, readAt)
	if err != nil {
		return AggregateCount{}, err
	}
	args := []any{
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
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		readAt,
	}
	args = append(args, supplyChainImpactCallerAuthorizationArgs(ctx)...)
	query := AggregateCombinedCountQuery
	switch {
	case s.readFromSupportsFor(filter):
		query = AggregateSupportsCombinedCountQuery
	case serveWinners:
		query = AggregateWinnersCombinedCountQuery
	}
	row := s.queryer().QueryRowContext(ctx, query, args...)
	var total, affected, affectedExact, affectedDerived, possiblyAffected, notAffected sql.NullInt64
	var priorityBuckets, severityBuckets []byte
	if err := row.Scan(&total, &affected, &affectedExact, &affectedDerived, &possiblyAffected, &notAffected, &priorityBuckets, &severityBuckets); err != nil {
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
		Freshness:        freshness,
	}

	if err := decodeAggregateBucketJSON(priorityBuckets, count.ByPriorityBucket); err != nil {
		return AggregateCount{}, fmt.Errorf("decode supply chain impact priority buckets: %w", err)
	}
	if err := decodeAggregateBucketJSON(severityBuckets, count.BySeverity); err != nil {
		return AggregateCount{}, fmt.Errorf("decode supply chain impact severity buckets: %w", err)
	}
	return count, nil
}

// decodeAggregateBucketJSON decodes the jsonb_agg(jsonb_build_object(...))
// bucket array from AggregateCombinedCountQuery into out. A NULL or empty
// payload means no buckets and leaves out empty.
func decodeAggregateBucketJSON(raw []byte, out map[string]int) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var rows []struct {
		Bucket string `json:"bucket"`
		Count  int    `json:"bucket_count"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		out[row.Bucket] = row.Count
	}
	return nil
}

// SupplyChainImpactInventory returns a paginated grouped count along the
// requested dimension. Limit and offset must already be normalized by the
// caller. A winners-eligible call probes the maintainer watermark first and
// fails closed with ErrAggregateReadModelUnavailable when the model cannot
// serve an exact answer; the page carries the freshness the handler
// discloses.
func (s PostgresAggregateStore) SupplyChainImpactInventory(
	ctx context.Context,
	filter AggregateFilter,
	dimension InventoryDimension,
	limit int,
	offset int,
) (AggregateInventoryPage, error) {
	if s.DB == nil && s.reader == nil {
		return AggregateInventoryPage{}, fmt.Errorf("supply chain impact aggregate database is required")
	}
	groupExpr, err := supplyChainImpactInventoryGroupExpression(dimension)
	if err != nil {
		return AggregateInventoryPage{}, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe (mirrors
	// PostgresFindingStore.ListSupplyChainImpactFindings).
	if limit <= 0 || limit > AggregateMaxLimit+1 {
		return AggregateInventoryPage{}, fmt.Errorf("limit must be between 1 and %d for internal pagination", AggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	readAt := SuppressionReadAt(s.Now)
	freshness, serveWinners, err := s.resolveAggregateFreshness(ctx, filter, readAt)
	if err != nil {
		return AggregateInventoryPage{}, err
	}
	q := InventoryQuery(groupExpr)
	switch {
	case s.readFromSupportsFor(filter):
		q = InventorySupportsQuery(groupExpr)
	case serveWinners:
		q = AggregateWinnersInventoryQuery(groupExpr)
	}
	args := []any{
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
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		readAt,
	}
	args = append(args, supplyChainImpactCallerAuthorizationArgs(ctx)...)
	args = append(args, limit, offset)
	rows, err := s.queryer().QueryContext(ctx, q, args...)
	if err != nil {
		return AggregateInventoryPage{}, fmt.Errorf("inventory supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := AggregateInventoryPage{
		Rows:      make([]InventoryRow, 0, limit),
		Freshness: freshness,
	}
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return AggregateInventoryPage{}, fmt.Errorf("scan supply chain impact inventory row: %w", err)
		}
		page.Rows = append(page.Rows, InventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return AggregateInventoryPage{}, fmt.Errorf("iterate supply chain impact inventory rows: %w", err)
	}
	return page, nil
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

// InventorySupportsQuery is InventoryQuery over the supports CTE, the read a
// granted call takes (#10880 PR-B2a).
func InventorySupportsQuery(groupExpr string) string {
	return strings.Replace(
		InventorySupportsQueryTemplate,
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
