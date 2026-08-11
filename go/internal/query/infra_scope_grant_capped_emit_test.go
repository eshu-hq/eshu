// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const scopeGrantCappedMetric = "eshu_dp_query_scope_grant_inline_capped_total"

// TestRecordScopeGrantInlineCapEmitsOncePerRead is the #5408 behaviour proof.
//
// The counter answers "how many scoped reads came back incomplete", so it has
// to increment once per READ. A single request builds the SHAPE-A disjunction
// more than once — infraSearchScopeClause calls scopeGrantInlineScalars three
// times — so an implementation that emitted from the clause builders would
// report one degraded read as three and quietly inflate the rate an operator
// pages on.
//
// It also must not fire at all for a token under the cap, or the signal stops
// meaning "degraded" and starts meaning "scoped".
func TestRecordScopeGrantInlineCapEmitsOncePerRead(t *testing.T) {
	t.Parallel()

	ids := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("repo-%d", i))
		}
		return out
	}

	for _, tc := range []struct {
		name      string
		filter    repositoryAccessFilter
		wantCount int64
	}{
		{
			name:      "over the cap emits exactly one",
			filter:    repositoryAccessFilter{allowedRepositoryIDs: ids(maxScopeGrantInlineTerms + 1)},
			wantCount: 1,
		},
		{
			name:   "at the cap is not a degradation",
			filter: repositoryAccessFilter{allowedRepositoryIDs: ids(maxScopeGrantInlineTerms)},
		},
		{
			name:   "all-scopes caller never emits",
			filter: repositoryAccessFilter{allScopes: true, allowedRepositoryIDs: ids(maxScopeGrantInlineTerms * 2)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("meter provider shutdown: %v", err)
				}
			})
			instruments, err := telemetry.NewInstruments(provider.Meter("scope-grant-cap-test"))
			if err != nil {
				t.Fatalf("telemetry.NewInstruments() error = %v", err)
			}

			recordScopeGrantInlineCap(context.Background(), instruments, tc.filter, "infra_search")

			if got := sumScopeGrantCapped(t, reader); got != tc.wantCount {
				t.Fatalf("%s = %d, want %d", scopeGrantCappedMetric, got, tc.wantCount)
			}
			if tc.wantCount > 0 {
				assertScopeGrantCappedLabel(t, reader, "infra_search")
			}
		})
	}
}

// TestRecordScopeGrantInlineCapSurvivesNilDependencies pins that the emitter is
// safe on the paths that have no telemetry wired. A handler constructed without
// Instruments is normal in tests and in the local profile, and an operator
// signal must never be the thing that panics a read.
func TestRecordScopeGrantInlineCapSurvivesNilDependencies(t *testing.T) {
	t.Parallel()

	over := repositoryAccessFilter{allowedRepositoryIDs: make([]string, 0, maxScopeGrantInlineTerms+1)}
	for i := 0; i < maxScopeGrantInlineTerms+1; i++ {
		over.allowedRepositoryIDs = append(over.allowedRepositoryIDs, fmt.Sprintf("repo-%d", i))
	}

	// Nil instruments, an Instruments with no counter registered, and an empty
	// surface must all be survivable.
	recordScopeGrantInlineCap(context.Background(), nil, over, "")
	recordScopeGrantInlineCap(context.Background(), &telemetry.Instruments{}, over, "infra_search")
}

// assertScopeGrantCappedLabel pins the wire label KEY and value, not just that
// the counter moved.
//
// The first revision of this test summed data-point values and never inspected
// attributes. That let a real contract bug through: the emitter used
// telemetry.AttrReason, whose wire key is "reason", while the metric
// description and the telemetry-coverage row both documented the label as
// "surface". An operator's PromQL written against the published contract —
// {surface="infra_search"} — would have matched no series, and the test stayed
// green because it only ever asked "did the counter increment?".
func assertScopeGrantCappedLabel(t *testing.T, reader *sdkmetric.ManualReader, wantSurface string) {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != scopeGrantCappedMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				got, ok := dp.Attributes.Value(telemetry.MetricDimensionReason)
				if !ok {
					t.Fatalf(
						"%s data point has no %q attribute; the documented label key and the emitted one have diverged",
						scopeGrantCappedMetric, telemetry.MetricDimensionReason,
					)
				}
				if got.AsString() != wantSurface {
					t.Fatalf("%s %s = %q, want %q", scopeGrantCappedMetric, telemetry.MetricDimensionReason, got.AsString(), wantSurface)
				}
			}
			return
		}
	}
	t.Fatalf("%s not found in collected metrics", scopeGrantCappedMetric)
}

// sumScopeGrantCapped totals the capped counter across data points, so a test
// cannot pass by matching one attribute set while another also incremented.
func sumScopeGrantCapped(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	total := int64(0)
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != scopeGrantCappedMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}
