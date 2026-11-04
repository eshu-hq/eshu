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
	"regexp"
	"strings"
	"testing"
	"time"

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
}

func (q *groupedFakeQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.calls = append(q.calls, groupedFakeCall{query: query, args: args})
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

func TestGroupedInventoryStoreBindsAggregateArgumentsInOrder(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-time.Minute)}}
	q.page = [][]any{groupedRow("service:a", false, 1, 10, `{"affected_exact":1}`, `{"high":1}`, false, 1)}
	filter := AggregateFilter{
		CVEID: "CVE-1", PackageID: "pkg", RepositoryID: "repository:r", SubjectDigest: "sha256:d",
		Status: "affected_exact", AdvisoryID: "GHSA-1", Ecosystem: "npm", ServiceID: "service:a",
		WorkloadID: "workload:w", Environment: "prod", Severity: "high", DetectionProfile: "precise",
		PriorityBucket: "high", MinPriorityScore: 7, SuppressionState: "active", IncludeSuppressed: true,
		ImageRef: "img", AllowedRepositoryIDs: []string{"repository:r"}, AllowedScopeIDs: []string{"scope:s"},
	}
	if _, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), filter, GroupedInventoryByService, 11, 20); err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if len(q.calls) != 2 || q.calls[0].query != SelectWinnersWatermarkQuery {
		t.Fatalf("calls = %d, want the watermark probe then the grouped query", len(q.calls))
	}
	args := q.calls[1].args
	want := []any{
		"CVE-1", "pkg", "repository:r", "sha256:d", "affected_exact", "GHSA-1", "npm", "service:a",
		"workload:w", "prod", "high", "precise", "high", 7, "active", true, "img",
	}
	if len(args) != groupedInventoryArgCount {
		t.Fatalf("args = %d, want %d", len(args), groupedInventoryArgCount)
	}
	for i, value := range want {
		if args[i] != value {
			t.Fatalf("arg $%d = %#v, want %#v", i+1, args[i], value)
		}
	}
	if got, ok := args[19].(time.Time); !ok || !got.Equal(groupedTestNow) {
		t.Fatalf("arg $20 = %#v, want the single UTC read clock %v", args[19], groupedTestNow)
	}
	if args[20] != 11 || args[21] != 20 {
		t.Fatalf("limit/offset args = %#v/%#v, want 11/20", args[20], args[21])
	}
	assertGroupedPlaceholdersBound(t, q.calls[1].query, len(args))
}

func assertGroupedPlaceholdersBound(t *testing.T, query string, argCount int) {
	t.Helper()
	for n := 1; n <= argCount; n++ {
		if !strings.Contains(query, fmt.Sprintf("$%d", n)) {
			t.Fatalf("query never references $%d", n)
		}
	}
	if strings.Contains(query, fmt.Sprintf("$%d", argCount+1)) {
		t.Fatalf("query references $%d beyond the %d bound args", argCount+1, argCount)
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
		{"fresh watermark", &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-time.Minute)}, page: rows()}, FreshnessLabelFresh, GroupCompletenessComplete, true},
		{"stale watermark", &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-10 * time.Minute)}, page: rows()}, FreshnessLabelStale, GroupCompletenessReadModelUnavailable, true},
		{"probe error", &groupedFakeQueryer{watermarkErr: errors.New("boom"), page: rows()}, FreshnessLabelUnknown, GroupCompletenessReadModelUnavailable, false},
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
				(wantMissing == 1 && named.MissingEvidence[0] != MissingEvidenceReadinessUnavailable) {
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

// TestGroupedInventoryScopedSQLShape pins the scoped variant: counts read the
// same granted and filtered finding set as the unscoped query, and the
// hidden-repositories signal reads the static winners with only the grant
// bound ($18, $19), never a request filter, so completeness cannot act as a
// filter-dependent oracle on hidden findings.
func TestGroupedInventoryScopedSQLShape(t *testing.T) {
	hidden := groupedInventoryHiddenRepositoriesSQL
	for _, want := range []string{"FROM supply_chain_impact_canonical_winners AS w", "w.winner_scope_id NOT LIKE 'image://derived/%'", "$18", "$19"} {
		if !strings.Contains(hidden, want) {
			t.Fatalf("hidden-repositories CTE missing %q", want)
		}
	}
	for n := 1; n <= groupedInventoryArgCount; n++ {
		if n == 18 || n == 19 {
			continue
		}
		if regexp.MustCompile(fmt.Sprintf(`\$%d\b`, n)).MatchString(hidden) {
			t.Fatalf("hidden-repositories CTE binds request parameter $%d", n)
		}
	}
	for _, dimension := range []GroupedInventoryDimension{GroupedInventoryByService, GroupedInventoryByWorkload, GroupedInventoryByEnvironment} {
		scoped, err := groupedInventoryScopedQuery(dimension)
		if err != nil {
			t.Fatalf("groupedInventoryScopedQuery(%q) error = %v", dimension, err)
		}
		unscoped, err := groupedInventoryQuery(dimension)
		if err != nil {
			t.Fatalf("groupedInventoryQuery(%q) error = %v", dimension, err)
		}
		if !strings.Contains(scoped, supplyChainImpactReadModeSQL(hidden, false)) ||
			!strings.Contains(scoped, supplyChainImpactReadModeSQL(groupedInventoryFindingsCTE, false)) {
			t.Fatalf("%s scoped query lost the hidden-repositories CTE or the shared finding set", dimension)
		}
		if strings.Contains(unscoped, "grouped_hidden_repositories") {
			t.Fatalf("%s unscoped query gained the scoped partial signal", dimension)
		}
		assertGroupedPlaceholdersBound(t, scoped, groupedInventoryArgCount)
	}
}

func TestGroupedInventoryStoreSelectsScopedQueryForGrants(t *testing.T) {
	cases := []struct {
		name   string
		filter AggregateFilter
		scoped bool
	}{
		{"unscoped", AggregateFilter{}, false},
		{"repository grant", AggregateFilter{AllowedRepositoryIDs: []string{"repository:r"}}, true},
		{"scope grant", AggregateFilter{AllowedScopeIDs: []string{"scope:s"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow}, page: [][]any{{nil, nil, nil, nil, nil, nil, nil, int64(0)}}}
			if _, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), tc.filter, GroupedInventoryByService, 10, 0); err != nil {
				t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
			}
			got := strings.Contains(q.calls[1].query, "grouped_hidden_repositories")
			if got != tc.scoped {
				t.Fatalf("scoped query used = %v, want %v", got, tc.scoped)
			}
		})
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
	if page.Rows[0].Completeness != GroupCompletenessReadModelUnavailable || page.Freshness.State != FreshnessLabelUnknown {
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
