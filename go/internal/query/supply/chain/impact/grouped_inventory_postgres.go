// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WinnersFreshnessWindow bounds how old the winners watermark may be before a
// winners-sourced read is stale: the grouped read marks its named buckets
// read_model_unavailable, and the parent chain findings handler reports its
// list stale (it aliases this value as supplyChainImpactWinnersFreshnessWindow,
// so the two reads cannot disagree on the same watermark age).
const WinnersFreshnessWindow = 2 * time.Minute

// groupedInventoryWatermarkProbeFailedEvent is the event_name of the one WARN
// line a failed winners watermark probe emits. The operator also sees
// freshness.state unknown and every named bucket read_model_unavailable.
const groupedInventoryWatermarkProbeFailedEvent = "supply_chain_impact.grouped_inventory_watermark_probe_failed"

// groupedInventoryProbeErrorMaxBytes bounds the error text on that WARN line.
const groupedInventoryProbeErrorMaxBytes = 256

// PostgresGroupedInventoryStore is the GroupedInventoryStore over the
// maintained supply_chain_impact_canonical_winners read model (#9873). The
// measured read-time CTE source missed the 1.5 s p95 budget at 5x by an order
// of magnitude, so this store reads only the winners table and answers
// ErrGroupedInventoryReadModelUnavailable when ReadFromWinners is off; there is
// no fallback to the CTE.
//
// One call runs two statements on the queryer: the bounded watermark probe
// (SelectWinnersWatermarkQuery) and one grouped statement. Both are plain
// reads; the store takes no lock beyond the ACCESS SHARE every SELECT takes,
// holds no transaction, and shares no mutable state, so concurrent calls do
// not contend with each other or with the winners maintainer's writes.
//
// The store does not detect an empty scoped grant. Empty AllowedRepositoryIDs
// and AllowedScopeIDs mean unrestricted to the SQL, exactly as in
// PostgresAggregateStore; the handler answers the empty page for an empty grant
// (access.Empty()) before it calls the store.
//
// Known divergence: with a scope-id grant (AllowedScopeIDs) the winners table
// has already picked one winner per canonical key across every scope, and the
// grant applies to that winner's scope. The aggregate CTE applies the grant
// before choosing the winner, so a key whose winner sits in an ungranted scope
// while a lower-ranked copy sits in a granted one is counted by the count
// route and not here (measured at 0.2 to 0.5 percent of the unattributed bucket
// on the shim seed). The winners list read has the same property in its SQL;
// in production the list store routes scoped calls to the CTE, which this
// store must not do. Two more cases make DistinctFindings differ from the
// count route: stale winners (the count route reads live facts), and copies
// of one canonical key that differ on a field outside the key, such as
// image_ref, detection_profile, or priority_bucket, where a filter on that
// field tests only the chosen winner here but every copy in the count route.
type PostgresGroupedInventoryStore struct {
	DB     FindingQueryer
	reader db.Queryer
	// ReadFromWinners mirrors ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ. When
	// false every call answers ErrGroupedInventoryReadModelUnavailable.
	ReadFromWinners bool
	// Now supplies the single UTC clock for suppression expiry and the
	// freshness window in one call. It defaults to time.Now.
	Now func() time.Time
	// Logger receives the WARN line of a failed watermark probe. A nil
	// logger drops it; the response still reads freshness unknown.
	Logger *slog.Logger
	// Instruments receives the per-route read counter
	// (eshu_dp_supply_chain_impact_read_route_total). Nil skips the counter.
	Instruments *telemetry.Instruments
}

// NewPostgresGroupedInventoryStoreWithReadStore routes the grouped read
// through the guarded read-only port, gated by the winners read flag.
func NewPostgresGroupedInventoryStoreWithReadStore(
	reader db.Queryer,
	readFromWinners bool,
	logger *slog.Logger,
) PostgresGroupedInventoryStore {
	return PostgresGroupedInventoryStore{reader: reader, ReadFromWinners: readFromWinners, Logger: logger}
}

// readScope returns the queryer that runs a route's one read statement and the
// function that ends its scope (see PostgresFindingStore.readScope).
func (s PostgresGroupedInventoryStore) readScope(ctx context.Context, route ReadRoute) (db.Queryer, func(), error) {
	if !route.wantsJITOff() {
		return s.queryer(), func() {}, nil
	}
	queryer, _, end, err := withJITOff(ctx, s.reader, s.DB, s.queryer(), nil)
	return queryer, end, err
}

func (s PostgresGroupedInventoryStore) queryer() db.Queryer {
	if s.reader != nil {
		return s.reader
	}
	return legacyFindingQueryer{database: s.DB}
}

// SupplyChainImpactGroupedInventory returns one page of service, workload, or
// environment buckets ordered by max_priority_score descending, count
// descending, then value ascending. limit may be one above AggregateMaxLimit
// for the handler's truncation probe. DistinctFindings is the whole filtered
// finding set, which equals the count route's total_findings for the same
// filters (subject to the scope-id grant divergence on the type).
//
// Completeness per named bucket: read_model_unavailable when the watermark is
// stale or its probe failed; otherwise partial when a scoped grant hides one
// of the bucket's map rows or any static winner on a repository the bucket
// maps to; otherwise complete. Partial means some finding mapped to the
// bucket exists that the grant does not admit, independent of the request
// filters, and it discloses no reason. A scoped request runs
// groupedInventoryScopedQuery, whose hidden-repositories CTE reads the winners
// with only the static-winner and grant predicates; counts stay granted and
// filtered. Page truncation is applied by the handler.
// An absent watermark (the maintainer never materialized the table) answers
// ErrGroupedInventoryReadModelUnavailable, since an empty table would read as
// zero findings.
func (s PostgresGroupedInventoryStore) SupplyChainImpactGroupedInventory(
	ctx context.Context,
	filter AggregateFilter,
	dimension GroupedInventoryDimension,
	limit int,
	offset int,
) (GroupedInventoryPage, error) {
	if !s.ReadFromWinners {
		return GroupedInventoryPage{}, ErrGroupedInventoryReadModelUnavailable
	}
	if s.DB == nil && s.reader == nil {
		return GroupedInventoryPage{}, fmt.Errorf("supply chain impact grouped inventory database is required")
	}
	// One route function decides the read model and the freshness (read_route.go):
	// a grant and an image reference read the per-support table, an unscoped
	// precise call reads the winners table plus the supplement, and everything else
	// reads the winners table.
	route := filter.route(s.ReadFromWinners)
	countRoute(ctx, s.Instruments, route)
	build := groupedInventoryQuery
	switch route {
	case RouteLegacy, RouteWinners, RouteSupportsFindingID, RouteSupportsSelector:
		// unreachable for the legacy route (a gate-off store refused above); the
		// winners route uses the winners builder
	case RouteSupportsGrant:
		build = groupedInventorySupportsQuery
	case RouteSupportsImage:
		build = groupedInventoryImageQuery
	case RouteWinnersSupplement:
		build = groupedInventorySupplementQuery
	}
	query, err := build(dimension)
	if err != nil {
		return GroupedInventoryPage{}, err
	}
	if limit <= 0 || limit > AggregateMaxLimit+1 {
		return GroupedInventoryPage{}, fmt.Errorf("limit must be between 1 and %d for internal pagination", AggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}

	readAt := SuppressionReadAt(s.Now)
	freshness, available, probeFailed := GroupedInventoryFreshness{State: packet.FreshnessLabelFresh}, true, false
	if route.readsSupports() {
		// The per-support table is current by construction (the maintainer writes
		// it in the finding facts' transaction), so a supports-only route reports
		// fresh with no winners watermark. A table that is not ready refuses.
		probe := PostgresFindingStore{DB: s.DB, reader: s.reader, ReadFromWinners: true}
		if err := probe.requireSupportsReady(ctx); err != nil {
			s.logWatermarkProbeFailure(ctx, err)
			return GroupedInventoryPage{}, fmt.Errorf("%w: %w", ErrGroupedInventoryReadModelUnavailable, err)
		}
	}
	if route.servesWinners() {
		// The winners table is read (plain, or with the supplement): the older
		// source bounds the answer, so the winners watermark labels the page.
		var err error
		freshness, available, probeFailed, err = s.winnersFreshness(ctx, readAt)
		if err != nil {
			return GroupedInventoryPage{}, err
		}
	}
	// The duration sample is the read: it starts after the readiness and watermark
	// probes, so a refused call records nothing and an accepted call does not
	// include them.
	defer timeRoute(ctx, s.Instruments, route)()

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
		limit,
		offset,
	}
	queryer, endRead, err := s.readScope(ctx, route)
	if err != nil {
		return GroupedInventoryPage{}, fmt.Errorf("supply chain impact grouped inventory: %w", err)
	}
	defer endRead()
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return GroupedInventoryPage{}, fmt.Errorf("grouped inventory supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := GroupedInventoryPage{Rows: make([]GroupedInventoryRow, 0, limit), Freshness: freshness}
	for rows.Next() {
		row, distinct, ok, err := scanGroupedInventoryRow(rows, dimension, available)
		if err != nil {
			return GroupedInventoryPage{}, err
		}
		page.DistinctFindings = distinct
		if ok {
			page.Rows = append(page.Rows, row)
		}
	}
	if err := rows.Err(); err != nil {
		return GroupedInventoryPage{}, fmt.Errorf("iterate supply chain impact grouped inventory rows: %w", err)
	}
	if probeFailed && len(page.Rows) == 0 {
		// A failed probe cannot tell a never-built winners table from a fresh
		// empty one, and an empty 200 would read as "no findings".
		return GroupedInventoryPage{}, ErrGroupedInventoryReadModelUnavailable
	}
	return page, nil
}

// winnersFreshness probes the winners watermark. It reports available=false
// for a stale watermark or a probe error (the rows are still served, marked
// read_model_unavailable), probeFailed=true for a probe error (logged once at
// WARN), and returns ErrGroupedInventoryReadModelUnavailable when no
// watermark row exists yet.
func (s PostgresGroupedInventoryStore) winnersFreshness(
	ctx context.Context,
	readAt time.Time,
) (freshness GroupedInventoryFreshness, available, probeFailed bool, err error) {
	probe := PostgresFindingStore{DB: s.DB, reader: s.reader, ReadFromWinners: true}
	watermark, probeErr := probe.SupplyChainImpactWinnersWatermark(ctx)
	if probeErr != nil {
		s.logWatermarkProbeFailure(ctx, probeErr)
		return GroupedInventoryFreshness{State: packet.FreshnessLabelUnknown}, false, true, nil
	}
	if !watermark.Present {
		return GroupedInventoryFreshness{}, false, false, ErrGroupedInventoryReadModelUnavailable
	}
	freshness = GroupedInventoryFreshness{
		State:            packet.FreshnessLabelFresh,
		LatestObservedAt: watermark.MaterializedAt.UTC().Format(time.RFC3339Nano),
	}
	if readAt.Sub(watermark.MaterializedAt.UTC()) > WinnersFreshnessWindow {
		freshness.State = packet.FreshnessLabelStale
		return freshness, false, false, nil
	}
	return freshness, true, false, nil
}

func (s PostgresGroupedInventoryStore) logWatermarkProbeFailure(ctx context.Context, err error) {
	if s.Logger == nil {
		return
	}
	text := err.Error()
	if len(text) > groupedInventoryProbeErrorMaxBytes {
		text = text[:groupedInventoryProbeErrorMaxBytes]
	}
	s.Logger.LogAttrs(
		ctx, slog.LevelWarn, "supply chain impact grouped inventory watermark probe failed",
		telemetry.EventAttr(groupedInventoryWatermarkProbeFailedEvent),
		slog.String("error", text),
	)
}

// scanGroupedInventoryRow decodes one result row. ok is false for the totals
// row of an empty page, which carries only distinct_findings.
func scanGroupedInventoryRow(
	rows db.Rows,
	dimension GroupedInventoryDimension,
	available bool,
) (GroupedInventoryRow, int, bool, error) {
	var (
		bucket       sql.NullString
		unattributed sql.NullBool
		count        sql.NullInt64
		maxPriority  sql.NullInt64
		byStatus     []byte
		bySeverity   []byte
		hidden       sql.NullBool
		distinct     int64
	)
	if err := rows.Scan(&bucket, &unattributed, &count, &maxPriority, &byStatus, &bySeverity, &hidden, &distinct); err != nil {
		return GroupedInventoryRow{}, 0, false, fmt.Errorf("scan supply chain impact grouped inventory row: %w", err)
	}
	if !bucket.Valid {
		return GroupedInventoryRow{}, int(distinct), false, nil
	}
	row := GroupedInventoryRow{
		Dimension: dimension,
		// SQL BTRIM already trimmed spaces; trimming more here would turn a
		// value SQL keeps (for example a tab) into a second unattributed row.
		Value:            bucket.String,
		Unattributed:     unattributed.Bool,
		Count:            int(count.Int64),
		MaxPriorityScore: int(maxPriority.Int64),
		ByImpactStatus:   map[string]int{},
		BySeverity:       map[string]int{},
		MissingEvidence:  []string{},
	}
	if err := decodeGroupedInventoryBreakdown(byStatus, row.ByImpactStatus); err != nil {
		return GroupedInventoryRow{}, 0, false, fmt.Errorf("decode grouped inventory impact status breakdown: %w", err)
	}
	if err := decodeGroupedInventoryBreakdown(bySeverity, row.BySeverity); err != nil {
		return GroupedInventoryRow{}, 0, false, fmt.Errorf("decode grouped inventory severity breakdown: %w", err)
	}
	switch {
	case row.Unattributed || row.Value == "":
		row.Unattributed = true
		row.Value = ""
		row.Completeness = GroupCompletenessUnattributed
		row.MissingEvidence = []string{UnattributedMissingEvidence(dimension)}
	case !available:
		row.Completeness = GroupCompletenessReadModelUnavailable
		row.MissingEvidence = []string{packet.MissingEvidenceReadinessUnavailable}
	case hidden.Bool:
		row.Completeness = GroupCompletenessPartial
	default:
		row.Completeness = GroupCompletenessComplete
	}
	return row, int(distinct), true, nil
}

func decodeGroupedInventoryBreakdown(raw []byte, into map[string]int) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, &into)
}
