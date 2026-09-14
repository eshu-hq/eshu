// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// tagHistoryScopedPagesByOutcome sums
// eshu_dp_query_container_image_tag_history_scoped_pages_total datapoints by
// their outcome attribute.
func tagHistoryScopedPagesByOutcome(t *testing.T, rm metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_container_image_tag_history_scoped_pages_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("scoped pages metric data = %T, want metricdata.Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(attribute.Key("outcome"))
				got[outcome.AsString()] += dp.Value
			}
		}
	}
	return got
}

// TestTagHistoryScopedPageRecordsCompleteOutcome proves an operator can see
// grant filtering happen on the public counter: the #6564 matrix keeps t1 and
// t4, withholds t2 and t3, and the page still completes inside its read budget.
// Not parallel: it installs a process-global meter provider.
func TestTagHistoryScopedPageRecordsCompleteOutcome(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	got := tagHistoryScopedPagesByOutcome(t, collectTagHistoryMetrics(t, reader))
	want := map[string]int64{tagHistoryScopedPageComplete: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped pages by outcome = %v, want %v", got, want)
	}
}

// TestTagHistoryScopedPageRecordsReadCapOutcome proves the one outcome an
// operator is actually paged for: a caller whose grant covers a thin slice of a
// busy tag exhausts taghistory.MaxRefillReads windows and the counter says so.
// That is the signal separating "this grant barely covers this tag" from "the
// route is slow", and it is the reason the withheld counts do not need to be on
// this surface to keep the route diagnosable.
func TestTagHistoryScopedPageRecordsReadCapOutcome(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	raw := taghistory.MaxRefillReads * taghistory.MaxLimit
	specs := make([]string, 0, raw+1)
	for range raw + 1 {
		specs = append(specs, "other")
	}
	w := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph(specs...),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=5",
	)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	got := tagHistoryScopedPagesByOutcome(t, collectTagHistoryMetrics(t, reader))
	want := map[string]int64{tagHistoryScopedPageReadCapReached: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped pages by outcome = %v, want %v", got, want)
	}
}

// TestTagHistoryUnscopedCallerRecordsNoScopedPages proves the counter stays
// silent for an unscoped caller, so it measures grant filtering only.
func TestTagHistoryUnscopedCallerRecordsNoScopedPages(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), nil, tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := tagHistoryScopedPagesByOutcome(t, collectTagHistoryMetrics(t, reader)); len(got) != 0 {
		t.Fatalf("scoped pages by outcome = %v, want none for an unscoped caller", got)
	}
}

// TestTagHistoryMetricsDiscloseNoWithheldCounts is the side-channel guard.
//
// /metrics bypasses authentication -- it is a literal entry in publicHTTPPaths
// (auth.go) and the middleware returns early for it before any token handling --
// and it is served from the same admin mux as the API, so a scoped caller can
// scrape it. Publishing this request's withheld row counts there would hand that
// caller, via a before/after scrape on a quiet deployment, the number its own
// response body declines to give it.
//
// The served request withholds two of four rows (the #6564 grant matrix), so if
// any exported datapoint carried a withheld count it would be visible below.
// The assertion is deliberately on the whole exported metric set and not on one
// metric name: renaming the counter, or folding the same numbers into a
// different instrument, must not make this pass.
func TestTagHistoryMetricsDiscloseNoWithheldCounts(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	rm := collectTagHistoryMetrics(t, reader)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if strings.Contains(m.Name, "withheld") {
				t.Fatalf("exported metric %q names a withheld count; /metrics is unauthenticated", m.Name)
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					if strings.Contains(kv.Value.AsString(), "withheld") {
						t.Fatalf(
							"exported metric %q carries attribute %s=%q; a scoped caller can scrape /metrics and recover its own page's withheld count",
							m.Name, kv.Key, kv.Value.AsString(),
						)
					}
				}
			}
		}
	}
}

// TestTagHistoryScopedSpanCarriesWithheldCounts is the other half of the guard
// above, and it is why removing the withheld dispositions from /metrics is a
// relocation rather than a deletion. The counts an operator needs to diagnose
// grant filtering at 3 AM must still exist -- on the span, which reaches them
// through the OTLP trace exporter rather than a public scrape, and which carries
// the per-request context a fleet counter never could.
//
// Without this test "move it to the span" would be an unproven claim: nothing
// else in the package asserts these attributes are written at all.
func TestTagHistoryScopedSpanCarriesWithheldCounts(t *testing.T) {
	recorder := withTagHistorySpanRecorder(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	attrs := tagHistorySpanAttributes(t, recorder)
	want := map[string]int64{
		spanAttrTagHistoryKeptCount:            2,
		spanAttrTagHistoryUngrantedCount:       1,
		spanAttrTagHistoryUnattributedCount:    1,
		spanAttrTagHistoryPreviousBlankedCount: 1,
	}
	for key, wantValue := range want {
		value, ok := attrs[key]
		if !ok {
			t.Fatalf("span attribute %q missing; the withheld counts have no other home", key)
		}
		if value.AsInt64() != wantValue {
			t.Fatalf("span attribute %s = %d, want %d", key, value.AsInt64(), wantValue)
		}
	}
	if got, ok := attrs[spanAttrTagHistoryGrantFiltered]; !ok || !got.AsBool() {
		t.Fatalf("span attribute %s = %v (present=%t), want true", spanAttrTagHistoryGrantFiltered, got, ok)
	}
	if _, ok := attrs[spanAttrTagHistoryRefillReads]; !ok {
		t.Fatalf("span attribute %s missing; refill work has no other home either", spanAttrTagHistoryRefillReads)
	}
}
