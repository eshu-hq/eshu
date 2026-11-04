// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
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
		{rows: [][]any{manifestDependencyFakeRow(0)}}, // active page
		{}, // delta-base page
		{rows: [][]any{{"repository:repo-1", "generation-1", "c0", ""}}}, // delta-target lookup
		{err: context.DeadlineExceeded},                                  // base resolution
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
// walks the union path to its end and pins the #9586 rule on the two batched
// auxiliary reads: both bind large arrays, and the overlay inputs read
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
		{rows: [][]any{base}},   // delta-base page
		{rows: [][]any{{"repository:repo-1", "delta-1", "c0", ""}}}, // delta-target lookup
		{rows: [][]any{{"base-1", "active"}}},                       // base lookup; active base, no prune probe
		{rows: [][]any{repository}},                                 // overlay inputs
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
	for i, want := range map[int]string{2: manifestDeltaTargetsQuery, 4: manifestDeltaOverlayInputsQuery} {
		call := db.queries[i]
		if call.query != want {
			t.Fatalf("call %d = %s, want %s", i+1, call.query, want)
		}
		if !call.hasExecMode || call.execMode != pgx.QueryExecModeCacheDescribe {
			t.Fatalf("call %d exec mode = %v (set %v), want QueryExecModeCacheDescribe", i+1, call.execMode, call.hasExecMode)
		}
	}
}
