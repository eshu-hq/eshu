// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestBuildLiveActivityQueryAllScopesMatchesOriginalShape asserts that the
// admin/all-scopes path (#5137 cold-review P1-1) produces the exact query
// shape backing the #5137 operations board proof: a status IN filter that
// engages fact_work_items_status_idx, deterministic ordering, and a
// placeholder LIMIT (the caller passes limit+1 so ReadLiveActivity can
// compute `truncated` without a second COUNT query) -- with NO access-scope
// predicate, so the admin plan shape is unchanged from before this fix.
func TestBuildLiveActivityQueryAllScopesMatchesOriginalShape(t *testing.T) {
	t.Parallel()

	query, args := buildLiveActivityQuery(101, true, nil, nil)
	for _, want := range []string{
		"FROM fact_work_items w",
		"JOIN ingestion_scopes s ON s.scope_id = w.scope_id",
		"WHERE w.status IN ('claimed', 'running', 'retrying')",
		"ORDER BY w.updated_at DESC, w.work_item_id",
		"LIMIT $1",
		"COALESCE(w.lease_owner, '') AS lease_owner",
		"COALESCE(NULLIF(BTRIM(s.payload->>'repo_slug'), ''), NULLIF(BTRIM(s.payload->>'repo_name'), ''), s.source_key) AS source_display",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("buildLiveActivityQuery(allScopes=true) missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "ANY(") {
		t.Fatalf("buildLiveActivityQuery(allScopes=true) must carry no access-scope predicate, got:\n%s", query)
	}
	if !reflect.DeepEqual(args, []any{101}) {
		t.Fatalf("buildLiveActivityQuery(allScopes=true) args = %#v, want [101] (limit only)", args)
	}
}

// TestBuildLiveActivityQueryAppliesScopePredicateForGrantedAccess is the
// cold-review P1-1 regression: a scoped caller with granted repository/scope
// ids must get an additional AND clause restricting rows to those grants,
// matching admin_store_dead_letters.go's buildListDeadLetterWorkItemsQuery
// shape over the same two tables (fact_work_items/ingestion_scopes).
func TestBuildLiveActivityQueryAppliesScopePredicateForGrantedAccess(t *testing.T) {
	t.Parallel()

	query, args := buildLiveActivityQuery(101, false, []string{"repo-a"}, []string{"scope-a"})
	const wantPredicate = "AND ((s.scope_kind = 'repository' AND s.source_key = ANY($2)) OR w.scope_id = ANY($3))"
	if !strings.Contains(query, wantPredicate) {
		t.Fatalf("buildLiveActivityQuery(allScopes=false) missing %q:\n%s", wantPredicate, query)
	}
	// The predicate must appear before ORDER BY/LIMIT so it constrains the
	// scanned row set, not just decorate the tail of the query.
	if strings.Index(query, wantPredicate) > strings.Index(query, "ORDER BY") {
		t.Fatalf("access-scope predicate must precede ORDER BY, got:\n%s", query)
	}

	wantArgs := []any{101, pq.Array([]string{"repo-a"}), pq.Array([]string{"scope-a"})}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("buildLiveActivityQuery(allScopes=false) args = %#v, want %#v", args, wantArgs)
	}
}

func liveActivityFakeRow(
	workItemID, stage, status, domain, leaseOwner string,
	claimUntil any,
	attemptCount int64,
	updatedAt, createdAt time.Time,
	scopeKind, collectorKind, sourceSystem, sourceKey, sourceDisplay, generationState string,
) []any {
	return []any{
		workItemID, stage, status, domain, leaseOwner,
		claimUntil, attemptCount, updatedAt, createdAt,
		scopeKind, collectorKind, sourceSystem, sourceKey, sourceDisplay, generationState,
	}
}

// TestReadLiveActivityScansRowsAndJoinsScopeIdentity verifies a full row
// (including a live claim_until) scans into every LiveActivityRow field, so
// an operator sees the repo/collector identity, worker, and lease deadline
// for an in-flight item.
func TestReadLiveActivityScansRowsAndJoinsScopeIdentity(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	createdAt := updatedAt.Add(-5 * time.Minute)
	claimUntil := updatedAt.Add(90 * time.Second)

	const (
		opaqueSourceKey = "repository:r_ea78e8bb"
		repoDisplayName = "eshu-hq/eshu"
	)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{
				liveActivityFakeRow(
					"wi-1", "reducer", "claimed", "workload_materialization", "reducer-worker-1",
					claimUntil, 2, updatedAt, createdAt,
					"repository", "github", "github.com", opaqueSourceKey, repoDisplayName, "active",
				),
			}},
		},
	}

	store := NewLiveActivityStore(queryer)
	rows, truncated, err := store.ReadLiveActivity(context.Background(), 100, true, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}

	got := rows[0]
	want := struct {
		workItemID, stage, status, domain, leaseOwner                                     string
		attemptCount                                                                      int
		scopeKind, collectorKind, sourceSystem, sourceKey, sourceDisplay, generationState string
	}{"wi-1", "reducer", "claimed", "workload_materialization", "reducer-worker-1", 2, "repository", "github", "github.com", opaqueSourceKey, repoDisplayName, "active"}

	if got.WorkItemID != want.workItemID || got.Stage != want.stage || got.Status != want.status ||
		got.Domain != want.domain || got.LeaseOwner != want.leaseOwner || got.AttemptCount != want.attemptCount ||
		got.ScopeKind != want.scopeKind || got.CollectorKind != want.collectorKind ||
		got.SourceSystem != want.sourceSystem || got.SourceKey != want.sourceKey ||
		got.SourceDisplay != want.sourceDisplay || got.GenerationState != want.generationState {
		t.Fatalf("scanned row = %+v, want %+v", got, want)
	}
	if !got.ClaimUntil.Equal(claimUntil) {
		t.Fatalf("ClaimUntil = %v, want %v", got.ClaimUntil, claimUntil)
	}
	if !got.UpdatedAt.Equal(updatedAt) || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("UpdatedAt/CreatedAt = %v/%v, want %v/%v", got.UpdatedAt, got.CreatedAt, updatedAt, createdAt)
	}
}

// TestReadLiveActivityHandlesNullClaimUntil verifies a retrying item with no
// live lease (claim_until NULL) scans to a zero ClaimUntil rather than
// erroring, since retrying rows are not required to hold a claim. It doubles
// as the payload-fallback case: source_display equals source_key here,
// simulating what the query's COALESCE/NULLIF chain produces when the scope
// payload carries neither repo_slug nor repo_name. It also doubles as the
// #5138 "stale" scan case: a retrying row from a superseded generation
// scans generation_state = "stale" straight through to LiveActivityRow.
func TestReadLiveActivityHandlesNullClaimUntil(t *testing.T) {
	t.Parallel()

	const sourceKeyNoPayloadName = "eshu-hq/other"
	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{
				liveActivityFakeRow(
					"wi-2", "reducer", "retrying", "deployable_unit_correlation", "",
					nil, 3, updatedAt, updatedAt,
					"repository", "github", "github.com", sourceKeyNoPayloadName, sourceKeyNoPayloadName, "stale",
				),
			}},
		},
	}

	store := NewLiveActivityStore(queryer)
	rows, _, err := store.ReadLiveActivity(context.Background(), 100, true, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if !rows[0].ClaimUntil.IsZero() {
		t.Fatalf("ClaimUntil = %v, want zero for NULL claim_until", rows[0].ClaimUntil)
	}
	if rows[0].LeaseOwner != "" {
		t.Fatalf("LeaseOwner = %q, want empty for an unclaimed retrying item", rows[0].LeaseOwner)
	}
	if rows[0].SourceDisplay != sourceKeyNoPayloadName {
		t.Fatalf("SourceDisplay = %q, want it to fall back to SourceKey (%q) when the payload carries no repo name", rows[0].SourceDisplay, sourceKeyNoPayloadName)
	}
	if rows[0].GenerationState != "stale" {
		t.Fatalf("GenerationState = %q, want %q", rows[0].GenerationState, "stale")
	}
}

// TestReadLiveActivityMarksTruncatedWhenMoreRowsThanLimit verifies that when
// the query returns limit+1 rows (the store's own fetch strategy), the
// result is capped at limit and truncated is reported so the console can
// show "showing N of more" without a second COUNT query.
func TestReadLiveActivityMarksTruncatedWhenMoreRowsThanLimit(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	var rawRows [][]any
	for i := 0; i < 3; i++ {
		rawRows = append(rawRows, liveActivityFakeRow(
			"wi", "reducer", "claimed", "domain", "worker",
			nil, 1, updatedAt, updatedAt,
			"repository", "github", "github.com", "org/repo", "org/repo", "active",
		))
	}

	queryer := &fakeQueryer{responses: []fakeRows{{rows: rawRows}}}
	store := NewLiveActivityStore(queryer)

	rows, truncated, err := store.ReadLiveActivity(context.Background(), 2, true, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (capped at limit)", len(rows))
	}
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
}

// TestReadLiveActivityNotTruncatedWhenFewerRowsThanLimit verifies a small
// in-flight set (fewer rows than limit+1) is reported untruncated.
func TestReadLiveActivityNotTruncatedWhenFewerRowsThanLimit(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		liveActivityFakeRow("wi-1", "reducer", "claimed", "domain", "worker", nil, 1, updatedAt, updatedAt, "repository", "github", "github.com", "org/repo", "org/repo", "active"),
	}}}}

	store := NewLiveActivityStore(queryer)
	rows, truncated, err := store.ReadLiveActivity(context.Background(), 5, true, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
}

// TestReadLiveActivityDefaultsNonPositiveLimitTo100 proves a non-positive
// limit falls back to LiveActivityDefaultLimit rather than either returning
// zero rows (limit=0 taken literally) or an unbounded read: with 101 fake
// rows available, exactly LiveActivityDefaultLimit rows come back and
// truncated is true.
func TestReadLiveActivityDefaultsNonPositiveLimitTo100(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	var rawRows [][]any
	for i := 0; i < LiveActivityDefaultLimit+1; i++ {
		rawRows = append(rawRows, liveActivityFakeRow(
			"wi", "reducer", "claimed", "domain", "worker",
			nil, 1, updatedAt, updatedAt,
			"repository", "github", "github.com", "org/repo", "org/repo", "active",
		))
	}

	queryer := &fakeQueryer{responses: []fakeRows{{rows: rawRows}}}
	store := NewLiveActivityStore(queryer)

	rows, truncated, err := store.ReadLiveActivity(context.Background(), 0, true, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if len(rows) != LiveActivityDefaultLimit {
		t.Fatalf("len(rows) = %d, want %d (default limit)", len(rows), LiveActivityDefaultLimit)
	}
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
}

// TestReadLiveActivityPropagatesQueryError verifies a Postgres query failure
// surfaces as a wrapped error rather than an empty, misleadingly-successful
// result.
func TestReadLiveActivityPropagatesQueryError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection reset")
	queryer := &fakeQueryer{responses: []fakeRows{{err: wantErr}}}
	store := NewLiveActivityStore(queryer)

	rows, truncated, err := store.ReadLiveActivity(context.Background(), 100, true, nil, nil)
	if err == nil {
		t.Fatal("ReadLiveActivity() error = nil, want wrapped queryer error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReadLiveActivity() error = %v, want wrapping %v", err, wantErr)
	}
	if rows != nil || truncated {
		t.Fatalf("ReadLiveActivity() on error returned rows=%v truncated=%v, want nil/false", rows, truncated)
	}
}

// TestReadLiveActivityRequiresQueryer verifies a zero-value store (no
// queryer wired) fails loudly instead of silently returning empty activity,
// which would render an operator board as falsely idle.
func TestReadLiveActivityRequiresQueryer(t *testing.T) {
	t.Parallel()

	var store LiveActivityStore
	if _, _, err := store.ReadLiveActivity(context.Background(), 100, true, nil, nil); err == nil {
		t.Fatal("ReadLiveActivity() error = nil, want error for a nil queryer")
	}
}

// TestReadLiveActivityScopedWithGrantsIssuesFilteredQuery is the #5137
// cold-review P1-1 regression at the ReadLiveActivity boundary: a scoped
// caller with granted repository/scope ids must dispatch the access-scope
// predicate all the way down to the queryer, not just build it in isolation.
func TestReadLiveActivityScopedWithGrantsIssuesFilteredQuery(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		liveActivityFakeRow("wi-1", "reducer", "claimed", "domain", "worker", nil, 1, updatedAt, updatedAt, "repository", "github", "github.com", "org/repo", "org/repo", "active"),
	}}}}
	store := NewLiveActivityStore(queryer)

	rows, _, err := store.ReadLiveActivity(context.Background(), 100, false, []string{"repo-a"}, []string{"scope-a"})
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queryer received %d queries, want exactly 1", len(queryer.queries))
	}
	const wantPredicate = "AND ((s.scope_kind = 'repository' AND s.source_key = ANY($2)) OR w.scope_id = ANY($3))"
	if !strings.Contains(queryer.queries[0], wantPredicate) {
		t.Fatalf("dispatched query missing access-scope predicate %q:\n%s", wantPredicate, queryer.queries[0])
	}
}

// TestReadLiveActivityScopedEmptyGrantsShortCircuitsWithoutQuerying is the
// #5137 cold-review P1-1 core regression: a scoped caller with NO granted
// repository or ingestion scope must see zero in-flight rows and must never
// reach Postgres at all -- existence, volume, domain, and timing of another
// tenant's work items must never leak, even with identity fields already
// redacted at the query-handler layer. This is defense in depth alongside
// the query handler's own repositoryAccessFilter.empty() short-circuit (see
// getOperations in go/internal/query/status_operations.go).
func TestReadLiveActivityScopedEmptyGrantsShortCircuitsWithoutQuerying(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := NewLiveActivityStore(queryer)

	rows, truncated, err := store.ReadLiveActivity(context.Background(), 100, false, nil, nil)
	if err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil for a scoped caller with no grants", rows)
	}
	if truncated {
		t.Fatal("truncated = true, want false for a scoped caller with no grants")
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("queryer received %d queries, want 0 -- a scoped, grant-empty caller must never reach Postgres", len(queryer.queries))
	}
}

// TestNewInstrumentedLiveActivityStoreWiresInstruments verifies the
// instrumented constructor assigns the caller's Instruments, matching
// NewInstrumentedStatusStore's convention.
func TestNewInstrumentedLiveActivityStoreWiresInstruments(t *testing.T) {
	t.Parallel()

	instruments := &telemetry.Instruments{}
	store := NewInstrumentedLiveActivityStore(&fakeQueryer{}, instruments)
	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p)", store.Instruments, instruments)
	}
}

// TestReadLiveActivityRecordsDurationAndErrorMetrics verifies a successful
// read records eshu_dp_status_operations_live_activity_query_duration_seconds
// and a failed read increments
// eshu_dp_status_operations_live_activity_query_errors_total, so an operator
// dashboard actually observes this query's latency and error rate.
func TestReadLiveActivityRecordsDurationAndErrorMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	updatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	okQueryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		liveActivityFakeRow("wi-1", "reducer", "claimed", "domain", "worker", nil, 1, updatedAt, updatedAt, "repository", "github", "github.com", "org/repo", "org/repo", "active"),
	}}}}
	okStore := NewInstrumentedLiveActivityStore(okQueryer, instruments)
	if _, _, err := okStore.ReadLiveActivity(context.Background(), 100, true, nil, nil); err != nil {
		t.Fatalf("ReadLiveActivity() error = %v, want nil", err)
	}

	failQueryer := &fakeQueryer{responses: []fakeRows{{err: errors.New("boom")}}}
	failStore := NewInstrumentedLiveActivityStore(failQueryer, instruments)
	if _, _, err := failStore.ReadLiveActivity(context.Background(), 100, true, nil, nil); err == nil {
		t.Fatal("ReadLiveActivity() error = nil, want the queryer failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	var sawDuration, sawErrorCount bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "eshu_dp_status_operations_live_activity_query_duration_seconds":
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", m.Name, m.Data)
				}
				if len(histogram.DataPoints) == 0 {
					t.Fatalf("metric %s has no data points", m.Name)
				}
				// Both the success and the failure call record duration, so
				// the histogram observed exactly 2 samples.
				if histogram.DataPoints[0].Count != 2 {
					t.Fatalf("metric %s count = %d, want 2 (one per ReadLiveActivity call)", m.Name, histogram.DataPoints[0].Count)
				}
				sawDuration = true
			case "eshu_dp_status_operations_live_activity_query_errors_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
				}
				if len(sum.DataPoints) == 0 || sum.DataPoints[0].Value != 1 {
					t.Fatalf("metric %s data points = %+v, want a single point with value 1", m.Name, sum.DataPoints)
				}
				sawErrorCount = true
			}
		}
	}
	if !sawDuration {
		t.Fatal("duration histogram eshu_dp_status_operations_live_activity_query_duration_seconds not recorded")
	}
	if !sawErrorCount {
		t.Fatal("error counter eshu_dp_status_operations_live_activity_query_errors_total not recorded")
	}
}
