// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

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
