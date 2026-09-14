// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// tagHistoryPublicMetricSurface is the COMPLETE inventory of instruments the
// tag-history route may export to the unauthenticated /metrics scrape, mapped
// to the COMPLETE set of attribute keys each one may label a datapoint with.
//
// It is an allowlist, so the guard below fails CLOSED: a new instrument, or a
// new label on an existing one, fails the test until somebody adds it here
// deliberately -- whatever the instrument is named and whatever the label says.
// That is the property this side channel actually needs. Banning the literal
// "withheld" bans a WORD, not a disclosure: the same two numbers re-exported on
// the scoped-pages counter under disposition="ungranted" and
// disposition="unattributed" restore the exposure in full and spell no banned
// word anywhere.
var tagHistoryPublicMetricSurface = map[string][]string{
	"eshu_dp_query_container_image_tag_history_duration_seconds":   {"outcome", "service.namespace"},
	"eshu_dp_query_container_image_tag_history_errors_total":       {"reason", "service.namespace"},
	"eshu_dp_query_container_image_tag_history_scoped_pages_total": {"outcome", "service.namespace"},
}

// tagHistoryExportedPoint is one exported datapoint reduced to everything a
// scoped caller scraping /metrics can read off it: which series it belongs to,
// and the single number it carries.
//
// For a counter that number is the sum. For the latency histogram it is the
// OBSERVATION COUNT and deliberately not the latency sum or the bucket layout:
// those are wall clock, they differ run to run, and no number derived from the
// page's contents reaches them. Everything else about the histogram -- that it
// exists, under which name, under which label keys and values -- is compared.
type tagHistoryExportedPoint struct {
	Metric string
	Attrs  string
	Value  int64
}

// tagHistoryAttrKey renders one datapoint's attributes as a canonical
// "key=value,key=value" string, failing the test when the datapoint carries an
// attribute key its metric's allowlist does not permit.
func tagHistoryAttrKey(t *testing.T, metricName string, set attribute.Set, allowedKeys []string) string {
	t.Helper()
	pairs := make([]string, 0, set.Len())
	iter := set.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		if !slices.Contains(allowedKeys, string(kv.Key)) {
			t.Fatalf(
				"exported metric %q carries attribute key %q, which is not one of %v; an unreviewed label on an unauthenticated series is exactly how the withheld counts get back onto /metrics",
				metricName, kv.Key, allowedKeys,
			)
		}
		pairs = append(pairs, string(kv.Key)+"="+kv.Value.String())
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

// tagHistoryExportedSurface reduces one collection to the sorted list of points
// a /metrics scrape would publish.
//
// It fails the test on any instrument tagHistoryPublicMetricSurface does not
// name, on any attribute key it does not permit, and on any datapoint kind this
// reduction cannot read -- a new aggregation would otherwise be summarised as
// nothing at all, which is the vacuity the whole guard exists to avoid.
func tagHistoryExportedSurface(t *testing.T, rm metricdata.ResourceMetrics) []tagHistoryExportedPoint {
	t.Helper()
	points := []tagHistoryExportedPoint{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			allowedKeys, ok := tagHistoryPublicMetricSurface[m.Name]
			if !ok {
				t.Fatalf(
					"exported metric %q is not in tagHistoryPublicMetricSurface; /metrics is unauthenticated, so a new tag-history instrument must be reviewed for what a scoped caller can derive from it before it ships",
					m.Name,
				)
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					points = append(points, tagHistoryExportedPoint{
						Metric: m.Name,
						Attrs:  tagHistoryAttrKey(t, m.Name, dp.Attributes, allowedKeys),
						Value:  dp.Value,
					})
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					points = append(points, tagHistoryExportedPoint{
						Metric: m.Name,
						Attrs:  tagHistoryAttrKey(t, m.Name, dp.Attributes, allowedKeys),
						Value:  int64(dp.Count),
					})
				}
			default:
				t.Fatalf(
					"exported metric %q has data type %T, which this guard cannot read; teach tagHistoryExportedSurface to read it rather than letting an unauthenticated series go unchecked",
					m.Name, m.Data,
				)
			}
		}
	}
	slices.SortFunc(points, tagHistoryPointOrder)
	return points
}

// tagHistoryPointOrder is the canonical order of an exported surface: by
// metric name, then by rendered attributes. A collected surface and an
// expectation assembled by hand are both sorted with it, so a DeepEqual
// between them compares contents rather than the order they happen to be
// written in.
func tagHistoryPointOrder(a, b tagHistoryExportedPoint) int {
	if a.Metric != b.Metric {
		return strings.Compare(a.Metric, b.Metric)
	}
	return strings.Compare(a.Attrs, b.Attrs)
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
// The served request withholds two of four rows (the #6564 grant matrix), so a
// withheld count exported anywhere would be visible below. The assertion is the
// WHOLE exported surface pinned to an exact expected set -- every instrument,
// every label key, every label value and every number -- and deliberately not a
// search for a forbidden word. A vocabulary ban is the wrong shape: it stops a
// series called withheld_ungranted and waves through the identical numbers
// re-exported as disposition="ungranted".
//
// Pinning the surface instead means a new instrument, a new label key, a
// changed label value or a moved number fails here, whatever it is called,
// until somebody widens tagHistoryPublicMetricSurface and this expectation on
// purpose. The scope of that promise is exactly what this test SERVES: one
// successful scoped page. A label value only an error path emits reaches no
// assertion here and never did -- the request is answered 200, so the error
// counter contributes no datapoint for a DeepEqual to notice. Pinning those
// values is TestTagHistoryRefusalMetricsPinEveryErrorReason's job, in
// tag_history_telemetry_test.go.
//
// Two siblings carry the rest of the guard.
// TestTagHistoryMetricsDoNotVaryWithWithheldCounts proves no number on this
// surface MOVES with the withheld counts, where this test pins ONE page's
// surface, and the refusal test covers the reason values no successful page
// produces.
func TestTagHistoryMetricsDiscloseNoWithheldCounts(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	namespace := ",service.namespace=" + telemetry.DefaultServiceNamespace
	want := []tagHistoryExportedPoint{
		{
			Metric: "eshu_dp_query_container_image_tag_history_duration_seconds",
			Attrs:  "outcome=ok" + namespace,
			Value:  1,
		},
		{
			Metric: "eshu_dp_query_container_image_tag_history_scoped_pages_total",
			Attrs:  "outcome=" + tagHistoryScopedPageComplete + namespace,
			Value:  1,
		},
	}
	got := tagHistoryExportedSurface(t, collectTagHistoryMetrics(t, reader))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"exported metric surface = %v, want %v; /metrics is unauthenticated, so every number on it has to be one the caller already holds",
			got, want,
		)
	}
}

// tagHistoryScopedPageSurface serves one grant-filtered page over the seeded
// specs and returns the metric surface that page exported, together with the
// page's (raw rows read, rows kept) -- whose difference is how many rows the
// page withheld, and which is the vacuity control for the differential below.
//
// Each call installs its own manual reader, so two pages' surfaces are directly
// comparable rather than cumulative. The withheld count is taken from the fake
// graph and the response body rather than from the handler span, deliberately:
// queryHandlerTracer (handler_tracing.go) is seeded once at package init from
// the OTel global proxy, and that proxy binds a cached tracer to the FIRST
// provider installed in the process and never rebinds it, so a span recorder
// installed here would both record nothing for the second page and silently
// blind TestTagHistoryScopedSpanCarriesWithheldCounts below.
func tagHistoryScopedPageSurface(t *testing.T, specs ...string) ([]tagHistoryExportedPoint, [2]int) {
	t.Helper()
	reader := withTagHistoryMetricReader(t)

	graph := newSeededTagHistoryGraph(specs...)
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	kept := len(tagHistoryResultTags(t, decodeTagHistoryBody(t, w)))
	surface := tagHistoryExportedSurface(t, collectTagHistoryMetrics(t, reader))
	return surface, [2]int{graph.rawRowsRead, kept}
}

// TestTagHistoryMetricsDoNotVaryWithWithheldCounts proves the property the
// allowlist cannot: that no number on the unauthenticated surface MOVES with
// the withheld counts.
//
// Two pages are served whose grant counts differ in every component -- the
// first reads 4 rows and keeps 2, the second reads 7 and keeps 1 -- and their
// exported surfaces must be identical. An edit that folds a withheld count into
// an ALREADY ALLOWED series under an ALREADY ALLOWED label key satisfies
// tagHistoryPublicMetricSurface and fails here, which is the residue an
// allowlist on shape alone leaves open.
//
// The (read, kept) assertion is the vacuity control. Without it, two pages that
// happened to withhold the same number of rows would pass the comparison while
// proving nothing at all.
func TestTagHistoryMetricsDoNotVaryWithWithheldCounts(t *testing.T) {
	few, fewRows := tagHistoryScopedPageSurface(t, "granted", "granted", "other", "none")
	many, manyRows := tagHistoryScopedPageSurface(
		t, "granted", "other", "other", "other", "none", "none", "none",
	)

	got := [2][2]int{fewRows, manyRows}
	if want := [2][2]int{{4, 2}, {7, 1}}; got != want {
		t.Fatalf(
			"per-page (raw rows read, rows kept) = %v, want %v; the two pages must withhold 2 and 6 rows respectively or the comparison below is vacuous",
			got, want,
		)
	}
	if !reflect.DeepEqual(few, many) {
		t.Fatalf(
			"exported metric surface differs with the withheld counts:\n  4 read / 2 kept: %v\n  7 read / 1 kept: %v\na scoped caller scrapes /metrics, so any number that moves with what was withheld from it is the side channel reopened",
			few, many,
		)
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
