// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// partialCorpus is the dense-late class: the tail is cancelled and the budget
// ends the walk, so every signal fires.
func partialCorpus() *fakeCorpus {
	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%50 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	corpus.tailLag = 120 * ms
	return corpus
}

type signals struct {
	spans    *tracetest.SpanRecorder
	reader   *sdkmetric.ManualReader
	logs     *bytes.Buffer
	searcher *unscoped.Searcher
	clock    *fakeClock
}

func newSignals(t *testing.T, corpus *fakeCorpus) *signals {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	clock := newFakeClock()
	return &signals{
		spans:  spans,
		reader: reader,
		logs:   logs,
		clock:  clock,
		searcher: &unscoped.Searcher{
			Store:       newFakeDB(corpus, clock),
			Now:         clock.Now,
			Tracer:      provider.Tracer("test"),
			Instruments: instruments,
			Logger:      slog.New(slog.NewJSONHandler(logs, nil)),
		},
	}
}

func spanAttrs(t *testing.T, spans *tracetest.SpanRecorder) map[string]attribute.Value {
	t.Helper()
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	out := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		out[string(kv.Key)] = kv.Value
	}
	return out
}

func TestSearchRecordsSpanAttributesForPartial(t *testing.T) {
	t.Parallel()

	s := newSignals(t, partialCorpus())
	page, err := s.searcher.Search(context.Background(), "needle-secret-pattern", 200, 0, querycontract.SearchCursor{})
	if err != nil || page.Partial == nil {
		t.Fatalf("Search() = (%+v, %v), want a partial page", page, err)
	}
	attrs := spanAttrs(t, s.spans)
	checks := map[string]string{
		"search.scope":   "unscoped",
		"search.outcome": "partial",
		"db.operation":   "search_file_content_any_repo_page",
	}
	for key, want := range checks {
		if got := attrs[key].AsString(); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := attrs["search.budget_ms"].AsInt64(); got != 800 {
		t.Errorf("search.budget_ms = %d, want 800", got)
	}
	if got := attrs["search.overrun_ms"].AsInt64(); got != 120 {
		t.Errorf("search.overrun_ms = %d, want 120", got)
	}
	if !attrs["search.tail_ran"].AsBool() || !attrs["search.tail_cancelled"].AsBool() {
		t.Error("tail_ran and tail_cancelled must both be true")
	}
	if attrs["search.cursor_present"].AsBool() {
		t.Error("cursor_present = true, want false: the request carried no cursor")
	}
	if got := attrs["search.probe_rows_visited"].AsInt64(); got != 200 {
		t.Errorf("search.probe_rows_visited = %d, want 200", got)
	}
	if got := attrs["search.continuation_steps"].AsInt64(); got < 1 {
		t.Errorf("search.continuation_steps = %d, want at least 1", got)
	}
	if got := attrs["search.continuation_rows"].AsInt64(); got != attrs["search.continuation_steps"].AsInt64()*500 {
		t.Errorf("search.continuation_rows = %d, want steps x 500", got)
	}
	if got := attrs["search.elapsed_ms"].AsInt64(); got < 700 {
		t.Errorf("search.elapsed_ms = %d, want about the budget", got)
	}
	for key, value := range attrs {
		if strings.Contains(value.String(), "needle-secret-pattern") {
			t.Errorf("span attribute %s carries the search pattern", key)
		}
	}
}

func TestSearchRecordsMetricsAndPartialLogWithoutPattern(t *testing.T) {
	t.Parallel()

	s := newSignals(t, partialCorpus())
	if _, err := s.searcher.Search(context.Background(), "needle-secret-pattern", 200, 0, querycontract.SearchCursor{}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	var collected metricdata.ResourceMetrics
	if err := s.reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := map[string]int64{}
	histograms := map[string]uint64{}
	outcomes := map[string]string{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					counts[m.Name] += dp.Value
					if v, ok := dp.Attributes.Value("outcome"); ok {
						outcomes[m.Name] = v.AsString()
					}
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					histograms[m.Name] += dp.Count
				}
			}
		}
	}
	if counts["eshu_dp_content_search_unscoped_total"] != 1 || outcomes["eshu_dp_content_search_unscoped_total"] != "partial" {
		t.Errorf("unscoped counter = %d outcome=%q, want 1 partial", counts["eshu_dp_content_search_unscoped_total"], outcomes["eshu_dp_content_search_unscoped_total"])
	}
	if counts["eshu_dp_content_search_tail_cancel_total"] != 1 {
		t.Errorf("tail cancel counter = %d, want 1", counts["eshu_dp_content_search_tail_cancel_total"])
	}
	for _, name := range []string{"eshu_dp_content_search_unscoped_duration_seconds", "eshu_dp_content_search_unscoped_overrun_seconds"} {
		if histograms[name] != 1 {
			t.Errorf("%s samples = %d, want 1", name, histograms[name])
		}
	}
	line := s.logs.String()
	for _, want := range []string{`"event_name":"content_search.unscoped_partial"`, `"reason":"budget_exceeded_on_large_document"`, `"overrun_ms":120`} {
		if !strings.Contains(line, want) {
			t.Errorf("partial log %s missing %s", line, want)
		}
	}
	if strings.Contains(line, "needle-secret-pattern") {
		t.Errorf("partial log carries the search pattern: %s", line)
	}
}

// An exact search logs nothing at partial level and counts as exact.
func TestSearchExactEmitsNoPartialLog(t *testing.T) {
	t.Parallel()

	s := newSignals(t, newCorpus(1000, 50*time.Microsecond, func(i int) bool { return i < 5 }))
	if _, err := s.searcher.Search(context.Background(), "needle", 2, 0, querycontract.SearchCursor{}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if s.logs.Len() != 0 {
		t.Fatalf("log = %s, want none", s.logs.String())
	}
	if got := spanAttrs(t, s.spans)["search.outcome"].AsString(); got != "exact" {
		t.Fatalf("search.outcome = %q, want exact", got)
	}
}

// Every timeout scales with the budget: at half the default the tail cap is
// half of 400 ms.
func TestSearchScalesTailTimeoutWithBudget(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
	corpus.tailCost = func(int) time.Duration { return 150 * ms }
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	page, fdb, _, err := runOn(t, fdb, clock, 400*ms, 50, 0, querycontract.SearchCursor{})
	if err != nil || page.Partial != nil {
		t.Fatalf("Search() = (%+v, %v), want an exact page", page, err)
	}
	assertTailShape(t, fdb.tx, "200ms")
}

// Every bounded statement and the statement timeout carry pgx's exec mode so
// the planner sees the bound values (a custom plan), never a cached generic one.
func TestSearchStatementsUseExecMode(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
	corpus.tailCost = func(int) time.Duration { return 334 * ms }
	_, fdb, _, err := run(t, corpus, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for i, label := range fdb.tx.log {
		if (label == "step" || label == "tail" || label == "set_timeout") && !fdb.tx.execMode[i] {
			t.Errorf("statement %d (%s) lacks QueryExecModeExec", i, label)
		}
	}
}
