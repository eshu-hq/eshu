// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// groupedFakeRows is a db.Rows double that assigns by destination type, so the
// store's nullable scan targets are exercised exactly as database/sql would.
type groupedFakeRows struct {
	rows    [][]any
	pos     int
	iterErr error
}

func (r *groupedFakeRows) Next() bool { return r.pos < len(r.rows) }

func (r *groupedFakeRows) Scan(dest ...any) error {
	row := r.rows[r.pos]
	r.pos++
	if len(dest) != len(row) {
		return fmt.Errorf("scan dest = %d, row = %d", len(dest), len(row))
	}
	for i, value := range row {
		switch target := dest[i].(type) {
		case *sql.NullString:
			*target = sql.NullString{}
			if value != nil {
				*target = sql.NullString{String: value.(string), Valid: true}
			}
		case *sql.NullBool:
			*target = sql.NullBool{}
			if value != nil {
				*target = sql.NullBool{Bool: value.(bool), Valid: true}
			}
		case *sql.NullInt64:
			*target = sql.NullInt64{}
			if value != nil {
				*target = sql.NullInt64{Int64: value.(int64), Valid: true}
			}
		case *[]byte:
			*target = nil
			if value != nil {
				*target = []byte(value.(string))
			}
		case *int64:
			*target = value.(int64)
		case *time.Time:
			*target = value.(time.Time)
		default:
			return fmt.Errorf("unexpected scan target %T", dest[i])
		}
	}
	return nil
}

func (r *groupedFakeRows) Err() error   { return r.iterErr }
func (r *groupedFakeRows) Close() error { return nil }

type groupedFakeCall struct {
	query string
	args  []any
}

// groupedFakeQueryer answers the watermark probe and the grouped query.
type groupedFakeQueryer struct {
	watermark    []time.Time
	watermarkErr error
	page         [][]any
	pageErr      error
	iterErr      error
	calls        []groupedFakeCall
	// supportsNotReady and supportsProbeErr shape the answer to the supports
	// readiness probe a granted call issues first.
	supportsNotReady bool
	supportsProbeErr error
}

func (q *groupedFakeQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.calls = append(q.calls, groupedFakeCall{query: query, args: args})
	if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
		if q.supportsProbeErr != nil {
			return nil, q.supportsProbeErr
		}
		return &cov7StoreRows{rows: [][]any{{!q.supportsNotReady}}}, nil
	}
	if query == SelectWinnersWatermarkQuery {
		if q.watermarkErr != nil {
			return nil, q.watermarkErr
		}
		rows := &groupedFakeRows{}
		for _, at := range q.watermark {
			rows.rows = append(rows.rows, []any{at})
		}
		return rows, nil
	}
	if q.pageErr != nil {
		return nil, q.pageErr
	}
	return &groupedFakeRows{rows: q.page, iterErr: q.iterErr}, nil
}

var groupedTestNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func groupedTestStore(q *groupedFakeQueryer) PostgresGroupedInventoryStore {
	store := NewPostgresGroupedInventoryStoreWithReadStore(q, true, nil)
	store.Now = func() time.Time { return groupedTestNow }
	return store
}

// groupedRow builds one result row in the store's column order: bucket,
// unattributed, count, max priority, by status, by severity, hidden by grant,
// distinct findings.
func groupedRow(bucket string, unattributed bool, count, maxPriority int64, status, severity string, hidden bool, distinct int64) []any {
	return []any{bucket, unattributed, count, maxPriority, status, severity, hidden, distinct}
}

func TestGroupedInventoryStoreGateOffReturnsTypedErrorWithoutQuery(t *testing.T) {
	q := &groupedFakeQueryer{}
	store := NewPostgresGroupedInventoryStoreWithReadStore(q, false, nil)
	_, err := store.SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0)
	if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
		t.Fatalf("err = %v, want ErrGroupedInventoryReadModelUnavailable", err)
	}
	if len(q.calls) != 0 {
		t.Fatalf("queries = %d, want 0 when the winners read gate is off", len(q.calls))
	}
}

func TestGroupedInventoryStoreRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name      string
		store     PostgresGroupedInventoryStore
		dimension GroupedInventoryDimension
		limit     int
	}{
		{"nil database", PostgresGroupedInventoryStore{ReadFromWinners: true}, GroupedInventoryByService, 10},
		{"unknown dimension", groupedTestStore(&groupedFakeQueryer{}), GroupedInventoryDimension("repository_id"), 10},
		{"zero limit", groupedTestStore(&groupedFakeQueryer{}), GroupedInventoryByService, 0},
		{"limit over probe", groupedTestStore(&groupedFakeQueryer{}), GroupedInventoryByService, AggregateMaxLimit + 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.store.SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, tc.dimension, tc.limit, 0)
			if err == nil {
				t.Fatal("err = nil, want an input error")
			}
			if errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
				t.Fatalf("err = %v, want an input error, not the read-model sentinel", err)
			}
		})
	}
}

func TestGroupedInventoryStoreDecodesBucketsAndCompleteness(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-30 * time.Second)}}
	q.page = [][]any{
		groupedRow("service:a", false, 3, 91, `{"affected_exact":2,"possibly_affected":1}`, `{"critical":1,"high":2}`, false, 5),
		groupedRow("service:b", false, 2, 70, `{"affected_derived":2}`, `{"none":2}`, true, 5),
		groupedRow("", true, 1, 40, `{"unknown":1}`, `{"medium":1}`, false, 5),
	}
	page, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0)
	if err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if page.DistinctFindings != 5 {
		t.Fatalf("DistinctFindings = %d, want 5", page.DistinctFindings)
	}
	if len(page.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(page.Rows))
	}
	a, b, unattributed := page.Rows[0], page.Rows[1], page.Rows[2]
	if a.Value != "service:a" || a.Count != 3 || a.MaxPriorityScore != 91 || a.Completeness != GroupCompletenessComplete {
		t.Fatalf("row a = %#v", a)
	}
	if a.ByImpactStatus["affected_exact"] != 2 || a.ByImpactStatus["possibly_affected"] != 1 || a.BySeverity["high"] != 2 {
		t.Fatalf("row a breakdowns = %#v / %#v", a.ByImpactStatus, a.BySeverity)
	}
	if a.Dimension != GroupedInventoryByService || len(a.MissingEvidence) != 0 {
		t.Fatalf("row a dimension/missing = %q/%v", a.Dimension, a.MissingEvidence)
	}
	if b.Completeness != GroupCompletenessPartial || len(b.MissingEvidence) != 0 {
		t.Fatalf("grant-hidden row = %#v, want partial with no disclosed reason", b)
	}
	if !unattributed.Unattributed || unattributed.Value != "" || unattributed.Completeness != GroupCompletenessUnattributed {
		t.Fatalf("unattributed row = %#v", unattributed)
	}
	if len(unattributed.MissingEvidence) != 1 || unattributed.MissingEvidence[0] != GroupedMissingEvidenceServiceMapping {
		t.Fatalf("unattributed missing evidence = %v", unattributed.MissingEvidence)
	}
}

// TestGroupedInventoryStoreDistinctFindingsStaysWholeSetOnTruncatedPage pins
// that DistinctFindings is the whole filtered set (the /impact/count total),
// not the findings of the returned buckets, even when the page is truncated.
func TestGroupedInventoryStoreDistinctFindingsStaysWholeSetOnTruncatedPage(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}}
	q.page = [][]any{
		groupedRow("service:a", false, 4, 90, `{"affected_exact":4}`, `{"high":4}`, false, 40),
		groupedRow("service:b", false, 3, 80, `{"affected_exact":3}`, `{"high":3}`, false, 40),
	}
	page, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByWorkload, 2, 0)
	if err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if page.DistinctFindings != 40 {
		t.Fatalf("DistinctFindings = %d, want the whole-set 40 on a truncated page", page.DistinctFindings)
	}
}

func TestGroupedInventoryStoreEmptyPageKeepsWholeSetDistinct(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}}
	q.page = [][]any{{nil, nil, nil, nil, nil, nil, nil, int64(12)}}
	page, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByEnvironment, 10, 500)
	if err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if len(page.Rows) != 0 || page.DistinctFindings != 12 {
		t.Fatalf("page = %d rows, distinct %d; want 0 rows, distinct 12", len(page.Rows), page.DistinctFindings)
	}
}

// TestGroupedInventoryStoreFreshnessStates covers the fresh, stale, and
// probe-error watermark states; the absent state is the typed error below.
func TestGroupedInventoryStoreFreshnessStates(t *testing.T) {
	rows := func() [][]any {
		return [][]any{
			groupedRow("prod", false, 2, 50, `{"affected_exact":2}`, `{"high":2}`, false, 3),
			groupedRow("", true, 1, 10, `{"affected_exact":1}`, `{"low":1}`, false, 3),
		}
	}
	cases := []struct {
		name             string
		q                *groupedFakeQueryer
		wantState        string
		wantCompleteness GroupCompleteness
		wantObservedAt   bool
	}{
		{"fresh watermark", &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-time.Minute)}, page: rows()}, packet.FreshnessLabelFresh, GroupCompletenessComplete, true},
		{"stale watermark", &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-10 * time.Minute)}, page: rows()}, packet.FreshnessLabelStale, GroupCompletenessReadModelUnavailable, true},
		{"probe error", &groupedFakeQueryer{watermarkErr: errors.New("boom"), page: rows()}, packet.FreshnessLabelUnknown, GroupCompletenessReadModelUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := groupedTestStore(tc.q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByEnvironment, 10, 0)
			if err != nil {
				t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
			}
			if page.Freshness.State != tc.wantState || (page.Freshness.LatestObservedAt != "") != tc.wantObservedAt {
				t.Fatalf("freshness = %#v, want state %q (observed_at present %v)", page.Freshness, tc.wantState, tc.wantObservedAt)
			}
			named, unattributed := page.Rows[0], page.Rows[1]
			if named.Completeness != tc.wantCompleteness {
				t.Fatalf("named completeness = %q, want %q", named.Completeness, tc.wantCompleteness)
			}
			wantMissing := 0
			if tc.wantCompleteness == GroupCompletenessReadModelUnavailable {
				wantMissing = 1
			}
			if len(named.MissingEvidence) != wantMissing ||
				(wantMissing == 1 && named.MissingEvidence[0] != packet.MissingEvidenceReadinessUnavailable) {
				t.Fatalf("named missing evidence = %v", named.MissingEvidence)
			}
			if unattributed.Completeness != GroupCompletenessUnattributed ||
				unattributed.MissingEvidence[0] != GroupedMissingEvidenceDeployEvidence {
				t.Fatalf("unattributed row = %#v, want pinned unattributed", unattributed)
			}
		})
	}
}

func TestGroupedInventoryStoreAbsentWatermarkIsUnavailable(t *testing.T) {
	q := &groupedFakeQueryer{}
	_, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0)
	if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
		t.Fatalf("err = %v, want ErrGroupedInventoryReadModelUnavailable before the winners model is materialized", err)
	}
	if len(q.calls) != 1 {
		t.Fatalf("queries = %d, want only the watermark probe", len(q.calls))
	}
}

func TestGroupedInventoryStoreWrapsQueryAndDecodeErrors(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}, pageErr: errors.New("conn reset")}
	if _, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0); err == nil ||
		!strings.Contains(err.Error(), "conn reset") {
		t.Fatalf("err = %v, want the wrapped query error", err)
	}
	iter := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}, iterErr: errors.New("stream cut")}
	if _, err := groupedTestStore(iter).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0); err == nil ||
		!strings.Contains(err.Error(), "stream cut") {
		t.Fatalf("err = %v, want the wrapped iteration error", err)
	}
	bad := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}}
	bad.page = [][]any{groupedRow("service:a", false, 1, 1, `not json`, `{}`, false, 1)}
	if _, err := groupedTestStore(bad).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0); err == nil {
		t.Fatal("err = nil, want a decode error for a malformed breakdown")
	}
}

// TestGroupedInventorySQLShape pins the source contract of every dimension:
// winners-sourced, static-winner predicate with no raw protected-mode marker,
// read-time expiry, the shared runtime filter, the dimension's runtime fact
// kind, and no per-finding operator-override join (the measured CTE cost).
func TestGroupedInventorySQLShape(t *testing.T) {
	kinds := map[GroupedInventoryDimension][]string{
		GroupedInventoryByService:     {"'reducer_service_catalog_correlation'"},
		GroupedInventoryByWorkload:    {"'reducer_service_catalog_correlation'", "'reducer_workload_identity'", "entity_keys"},
		GroupedInventoryByEnvironment: {"'reducer_ci_cd_run_correlation'"},
	}
	for dimension, wantKinds := range kinds {
		query, err := groupedInventoryQuery(dimension)
		if err != nil {
			t.Fatalf("groupedInventoryQuery(%q) error = %v", dimension, err)
		}
		for _, want := range append([]string{
			"FROM supply_chain_impact_canonical_winners AS w",
			"w.winner_scope_id NOT LIKE 'image://derived/%'",
			"$20::timestamptz",
			"runtime_filter_repositories",
			"ORDER BY bucket.max_priority DESC, bucket.bucket_count DESC, bucket.bucket ASC",
			"LIMIT $21 OFFSET $22",
		}, wantKinds...) {
			if !strings.Contains(query, want) {
				t.Fatalf("%s query missing %q", dimension, want)
			}
		}
		for _, banned := range []string{supplyChainImpactProtectedModeMarker, "operator_overrides", "service_ids", "workload_ids", "environments"} {
			if strings.Contains(query, banned) {
				t.Fatalf("%s query contains %q", dimension, banned)
			}
		}
		assertGroupedPlaceholdersBound(t, query, groupedInventoryArgCount)
	}
	if _, err := groupedInventoryQuery(GroupedInventoryDimension("ecosystem")); err == nil {
		t.Fatal("groupedInventoryQuery(ecosystem) error = nil, want unsupported dimension")
	}
}

// TestGroupedInventoryStoreKeepsSQLTrimmedValues pins that the store does not
// trim beyond SQL BTRIM: a tab-only value is a named bucket, not a second
// unattributed bucket.
func TestGroupedInventoryStoreKeepsSQLTrimmedValues(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}}
	q.page = [][]any{
		groupedRow("\t", false, 1, 10, `{"affected_exact":1}`, `{"high":1}`, false, 2),
		groupedRow("", true, 1, 5, `{"affected_exact":1}`, `{"low":1}`, false, 2),
	}
	page, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0)
	if err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if page.Rows[0].Value != "\t" || page.Rows[0].Unattributed || page.Rows[0].Completeness != GroupCompletenessComplete {
		t.Fatalf("tab bucket = %#v, want a named complete bucket", page.Rows[0])
	}
}

// TestGroupedInventoryStoreLogsWatermarkProbeFailure pins the operator signal
// for a failed watermark probe: one WARN line with a bounded event name, while
// the rows are still served as read_model_unavailable with freshness unknown.
func TestGroupedInventoryStoreLogsWatermarkProbeFailure(t *testing.T) {
	var logs bytes.Buffer
	q := &groupedFakeQueryer{watermarkErr: errors.New("conn reset by peer")}
	q.page = [][]any{groupedRow("prod", false, 1, 10, `{"affected_exact":1}`, `{"high":1}`, false, 1)}
	store := groupedTestStore(q)
	store.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	page, err := store.SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByEnvironment, 10, 0)
	if err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if page.Rows[0].Completeness != GroupCompletenessReadModelUnavailable || page.Freshness.State != packet.FreshnessLabelUnknown {
		t.Fatalf("page = %#v, want read_model_unavailable rows with unknown freshness", page)
	}
	line := logs.String()
	for _, want := range []string{`"level":"WARN"`, `"event_name":"` + groupedInventoryWatermarkProbeFailedEvent + `"`, "conn reset by peer"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log = %s, want %s", line, want)
		}
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("log lines = %d, want exactly one WARN line", strings.Count(line, "\n"))
	}
}

// TestGroupedInventoryStoreProbeFailureWithEmptyPageIsUnavailable pins the
// fail-closed case: when the probe fails and the grouped read finds no bucket,
// the store cannot tell a never-built winners table from a fresh empty one, so
// it answers the typed unavailable error instead of an empty page.
func TestGroupedInventoryStoreProbeFailureWithEmptyPageIsUnavailable(t *testing.T) {
	q := &groupedFakeQueryer{watermarkErr: errors.New("boom")}
	q.page = [][]any{{nil, nil, nil, nil, nil, nil, nil, int64(0)}}
	_, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), AggregateFilter{}, GroupedInventoryByService, 10, 0)
	if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
		t.Fatalf("err = %v, want ErrGroupedInventoryReadModelUnavailable for a probe failure with no bucket", err)
	}
}
