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

	"github.com/jackc/pgx/v5"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDeltaBasePackageManifestDependencyQueryDerivesEveryBranch pins the
// derivation contract for both shipped query texts: every branch swaps the
// active-generation join for the delta-base join and keeps the filters,
// cursor, ordering, and limit, and the impact snapshot rewrite still accepts
// the derived text.
func TestDeltaBasePackageManifestDependencyQueryDerivesEveryBranch(t *testing.T) {
	t.Parallel()

	for _, active := range []string{
		listActivePackageManifestDependencyFactsQuery,
		listActivePackageManifestDependencyFactsWithSBTQuery,
	} {
		branches := strings.Count(active, "FROM fact_records AS fact")
		derived, err := deltaBasePackageManifestDependencyQuery(active)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		for fragment, want := range map[string]int{
			manifestActiveScopeJoin:                                          0,
			manifestActiveGeneration:                                         0,
			manifestDeltaBaseJoin:                                            branches,
			manifestDeltaBaseFull:                                            branches,
			"ORDER BY fact.observed_at ASC, fact.fact_id ASC":                branches,
			"(fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)": branches,
			"AND fact.is_tombstone = FALSE":                                  branches,
		} {
			if got := strings.Count(derived, fragment); got != want {
				t.Fatalf("derived query carries %q %d times, want %d:\n%s", fragment, got, want, derived)
			}
		}
		snapshot := FactStore{impactStaticSnapshot: true}
		if _, err := snapshot.protectedImpactStaticFactSQL(derived); err != nil {
			t.Fatalf("impact snapshot rewrite refused the derived query: %v", err)
		}
	}
	if _, err := deltaBasePackageManifestDependencyQuery("SELECT 1"); err == nil {
		t.Fatal("derive(SELECT 1) error = nil, want the unsupported-shape error")
	}
}

// TestListActivePackageManifestDependencyFactsDeltaResolveFailureFails keeps
// a failed base lookup an error, counted union_resolve_failed, never a
// silent delta-only answer: only a #7797 refusal may fall back to the
// delta's rows.
func TestListActivePackageManifestDependencyFactsDeltaResolveFailureFails(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}},                    // active page
		{rows: [][]any{{"repository:repo-1", "generation-1", "c0", ""}}}, // delta-target lookup
		{},                              // delta-base page
		{err: context.DeadlineExceeded}, // base resolution
	}}
	_, err = NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read error = %v, want the base lookup's deadline error", err)
	}
	if got, want := db.queries[3].query, deltaUnionBaseQuery; got != want {
		t.Fatalf("call 4 = %s, want the #7797 base lookup", got)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var failed int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_package_manifest_dependency_read_delta_union_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				if outcome.AsString() == manifestDeltaUnionResolveFailed {
					failed += dp.Value
				}
			}
		}
	}
	if failed != 1 {
		t.Fatalf("union_resolve_failed = %d, want 1", failed)
	}
}

// TestListActivePackageManifestDependencyFactsDeltaAuxReadsUseCustomPlans
// walks the union path to its end and pins the #9586 rule on the base pages
// and the overlay inputs read: both bind large arrays, and the inputs read
// filters each delta's rows by stable_fact_key = ANY($3), which a generic
// plan compares linearly per row (1,077.8 ms against 14.9 ms custom on the
// #9991 corpus).
func TestListActivePackageManifestDependencyFactsDeltaAuxReadsUseCustomPlans(t *testing.T) {
	t.Parallel()

	active := manifestDependencyFakeRow(0)
	active[2] = "delta-1"
	base := manifestDependencyFakeRow(1)
	base[0], base[2] = "base-dep", "base-1"
	repository := []any{
		"delta-1:repository", "repository:repo-1", "delta-1", "repository", "repository:repo-1",
		"1.0.0", "git", int64(0), "unknown", "git", "repository:repo-1", "", "",
		time.Date(2026, time.May, 14, 11, 0, 0, 0, time.UTC), false,
		[]byte(`{"path":"/repo","delta_generation":true,"delta_relative_paths":["other/package.json"]}`),
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{active}}, // active page
		{rows: [][]any{{"repository:repo-1", "delta-1", "c0", ""}}}, // delta-target lookup
		{rows: [][]any{base}},                 // delta-base page
		{rows: [][]any{{"base-1", "active"}}}, // base lookup; active base, no prune probe
		{rows: [][]any{repository}},           // overlay inputs
	}}
	loaded, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].FactID != "fact-dep-0000" || loaded[1].FactID != "base-dep" {
		t.Fatalf("loaded = %+v, want [fact-dep-0000 base-dep]", loaded)
	}
	if len(db.queries) != 5 {
		t.Fatalf("query calls = %d, want 5", len(db.queries))
	}
	if db.queries[1].query != manifestDeltaTargetsQuery {
		t.Fatalf("call 2 = %s, want the delta-target lookup", db.queries[1].query)
	}
	if got := db.queries[2].args; len(got) != 6 || !reflect.DeepEqual(got[5], []string{"repository:repo-1"}) {
		t.Fatalf("delta-base page args = %#v, want $6 = the delta-active scope ids", got)
	}
	if db.queries[4].query != manifestDeltaOverlayInputsQuery {
		t.Fatalf("call 5 = %s, want the overlay inputs read", db.queries[4].query)
	}
	for _, i := range []int{2, 4} {
		call := db.queries[i]
		if !call.hasExecMode || call.execMode != pgx.QueryExecModeCacheDescribe {
			t.Fatalf("call %d exec mode = %v (set %v), want QueryExecModeCacheDescribe", i+1, call.execMode, call.hasExecMode)
		}
	}
}

// TestListActivePackageManifestDependencyFactsNoDeltaSkipsBaseRead pins the
// no-delta cost: when no scope anywhere has an active delta generation, the
// read sends its active pages plus one delta-target lookup and never the
// base-candidate scan, whose page costs as much as an active page.
func TestListActivePackageManifestDependencyFactsNoDeltaSkipsBaseRead(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}}, // active page
		{}, // delta-target lookup: no delta-active scope
	}}
	loaded, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d rows, want 1", len(loaded))
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("statements = %d, want %d (active page, delta-target lookup)", got, want)
	}
	if db.queries[1].query != manifestDeltaTargetsQuery {
		t.Fatalf("statement 2 = %s, want the delta-target lookup", db.queries[1].query)
	}
	for i, call := range db.queries {
		if strings.Contains(call.query, "JOIN scope_generations AS active_delta") {
			t.Fatalf("statement %d is the delta-base scan; want none without a delta-active scope", i+1)
		}
	}
}

// TestListActivePackageManifestDependencyFactsDeltaFailuresCountOncePerRead
// pins union_resolve_failed as one per read whose delta-union step failed,
// whichever statement failed: the target lookup, a base page, a base
// resolution, or the overlay inputs read.
func TestListActivePackageManifestDependencyFactsDeltaFailuresCountOncePerRead(t *testing.T) {
	t.Parallel()

	target := queueFakeRows{rows: [][]any{{"repository:repo-1", "generation-1", "c0", ""}}}
	active := queueFakeRows{rows: [][]any{manifestDependencyFakeRow(0)}}
	failed := queueFakeRows{err: context.DeadlineExceeded}
	baseRow := manifestDependencyFakeRow(1)
	baseRow[0], baseRow[2] = "base-dep", "base-1"
	basePage := queueFakeRows{rows: [][]any{baseRow}}
	activeBase := queueFakeRows{rows: [][]any{{"base-1", "active"}}} // active base: no prune probe
	for name, responses := range map[string][]queueFakeRows{
		"target lookup":   {active, failed},
		"base page":       {active, target, failed},
		"base resolution": {active, target, {}, failed},
		"overlay inputs":  {active, target, basePage, activeBase, failed},
	} {
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		db := &fakeExecQueryer{queryResponses: responses}
		if _, err := NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFacts(
			context.Background(), []string{"npm"}, []string{"left-pad"},
		); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: read error = %v, want the deadline error", name, err)
		}
		if got := manifestDeltaUnionCount(t, reader, manifestDeltaUnionResolveFailed); got != 1 {
			t.Fatalf("%s: union_resolve_failed = %d, want 1", name, got)
		}
		last := db.queries[len(db.queries)-1].query
		if want := map[string]string{
			"target lookup":   manifestDeltaTargetsQuery,
			"base resolution": deltaUnionBaseQuery,
			"overlay inputs":  manifestDeltaOverlayInputsQuery,
		}[name]; want != "" && last != want {
			t.Fatalf("%s: failing statement = %s, want %s", name, last, want)
		}
		if name == "base page" && !strings.Contains(last, "JOIN scope_generations AS active_delta") {
			t.Fatalf("base page: failing statement = %s, want the delta-base page", last)
		}
	}
}

// manifestDeltaUnionCount returns the delta-union counter's value for one
// outcome.
func manifestDeltaUnionCount(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_package_manifest_dependency_read_delta_union_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if value, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome); value.AsString() == outcome {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestListActivePackageManifestDependencyFactsNoBaselineSkipsBaseRead pins
// the baseline filter on the base scan: a delta-active scope with no recorded
// baseline commit is a legacy delta with no base to overlay, so the read
// sends no base-candidate page for it, returns the active rows unchanged, and
// counts union_no_baseline once.
func TestListActivePackageManifestDependencyFactsNoBaselineSkipsBaseRead(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0), manifestDependencyFakeRow(1)}}, // active page
		{rows: [][]any{{"repository:repo-1", "generation-1", "", ""}}},              // delta-target lookup: no baseline
	}}
	loaded, err := NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].FactID != "fact-dep-0000" || loaded[1].FactID != "fact-dep-0001" {
		t.Fatalf("loaded = %+v, want the two active rows unchanged", loaded)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("statements = %d, want %d (active page, delta-target lookup)", got, want)
	}
	for i, call := range db.queries {
		if strings.Contains(call.query, "JOIN scope_generations AS active_delta") {
			t.Fatalf("statement %d is the delta-base scan; want none for a delta with no baseline", i+1)
		}
	}
	if got := manifestDeltaUnionCount(t, reader, manifestDeltaUnionNoBaseline); got != 1 {
		t.Fatalf("union_no_baseline = %d, want 1", got)
	}
}
