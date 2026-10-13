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
	if got := attrs["search.phase_one_cap_ms"].AsInt64(); got != 440 {
		t.Errorf("search.phase_one_cap_ms = %d, want 440 at the default budget", got)
	}
	if got := attrs["search.ended_in"].AsString(); got != "tail" {
		t.Errorf("search.ended_in = %q, want tail: the cancelled tail is the last statement", got)
	}
	if got := attrs["search.steps_cancelled"].AsInt64(); got != 0 {
		t.Errorf("search.steps_cancelled = %d, want 0", got)
	}
	if !attrs["search.progressed"].AsBool() {
		t.Error("search.progressed = false, want true: the walk advanced the cursor")
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
	counts, histograms, outcomes := collectMetrics(t, s.reader)
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
	for _, name := range []string{"eshu_dp_content_search_step_cancel_total", "eshu_dp_content_search_no_progress_total"} {
		if counts[name] != 0 {
			t.Errorf("%s = %d, want 0: no step was cancelled and the walk progressed", name, counts[name])
		}
	}
	line := s.logs.String()
	for _, want := range []string{
		`"event_name":"content_search.unscoped_partial"`, `"reason":"budget_exceeded_on_large_document"`, `"overrun_ms":120`,
		`"progressed":true`, `"steps_cancelled":0`, `"ended_in":"tail"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("partial log %s missing %s", line, want)
		}
	}
	if strings.Contains(line, "needle-secret-pattern") {
		t.Errorf("partial log carries the search pattern: %s", line)
	}
}

// collectMetrics reads every counter sum, histogram sample count and the last
// outcome label per metric name.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) (map[string]int64, map[string]uint64, map[string]string) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
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
	return counts, histograms, outcomes
}

// A walk whose probe the server cancels and whose tail cannot finish advances
// nothing: the span, both new counters and the partial log line say so, and
// none of them carries the pattern.
func TestSearchCountsCancelledStepsAndNoProgress(t *testing.T) {
	t.Parallel()

	s := newSignals(t, stalledCorpus())
	page, err := s.searcher.Search(context.Background(), "needle-secret-pattern", 50, 0, querycontract.SearchCursor{})
	if err != nil || page.Partial == nil || page.Partial.Progressed {
		t.Fatalf("Search() = (%+v, %v), want a partial that made no progress", page, err)
	}
	attrs := spanAttrs(t, s.spans)
	if got := attrs["search.steps_cancelled"].AsInt64(); got != 1 {
		t.Errorf("search.steps_cancelled = %d, want 1 (the probe)", got)
	}
	if attrs["search.progressed"].AsBool() {
		t.Error("search.progressed = true, want false")
	}
	if got := attrs["search.ended_in"].AsString(); got != stalledEndedIn {
		t.Errorf("search.ended_in = %q, want %q", got, stalledEndedIn)
	}
	counts, _, _ := collectMetrics(t, s.reader)
	if got := counts["eshu_dp_content_search_step_cancel_total"]; got != 1 {
		t.Errorf("step cancel counter = %d, want 1", got)
	}
	if got := counts["eshu_dp_content_search_no_progress_total"]; got != 1 {
		t.Errorf("no-progress counter = %d, want 1", got)
	}
	line := s.logs.String()
	for _, want := range []string{`"progressed":false`, `"steps_cancelled":1`, `"ended_in":"` + stalledEndedIn + `"`} {
		if !strings.Contains(line, want) {
			t.Errorf("partial log %s missing %s", line, want)
		}
	}
	if strings.Contains(line, "needle-secret-pattern") {
		t.Errorf("partial log carries the search pattern: %s", line)
	}
}

// A no-trigram search that issues post-skip continuation steps still names
// the skip on search.ended_in: the skip's phase name survives the steps, so
// the signal names the decision that shaped the walk, not the last step.
func TestSearchNoTrigramSkipSurvivesContinuationSteps(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 500*time.Microsecond, func(i int) bool { return i%40 == 7 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	s := newSignals(t, corpus)
	page, err := s.searcher.Search(context.Background(), "db_", 200, 0, querycontract.SearchCursor{})
	if err != nil || page.Partial == nil {
		t.Fatalf("Search() = (%+v, %v), want a partial page", page, err)
	}
	attrs := spanAttrs(t, s.spans)
	if got := attrs["search.continuation_steps"].AsInt64(); got < 1 {
		t.Fatalf("search.continuation_steps = %d, want at least 1 post-skip step", got)
	}
	if got := attrs["search.ended_in"].AsString(); got != "tail_skipped_no_trigram" {
		t.Errorf("search.ended_in = %q, want tail_skipped_no_trigram after post-skip steps", got)
	}
	if attrs["search.tail_ran"].AsBool() {
		t.Error("search.tail_ran = true, want false: the tail was skipped")
	}
	if page.Partial.Reason != querycontract.SearchPartialPatternHasNoTrigram {
		t.Errorf("Reason = %q, want %q", page.Partial.Reason, querycontract.SearchPartialPatternHasNoTrigram)
	}
	line := s.logs.String()
	for _, want := range []string{`"reason":"pattern_has_no_trigram"`, `"ended_in":"tail_skipped_no_trigram"`, `"tail_ran":false`} {
		if !strings.Contains(line, want) {
			t.Errorf("partial log %s missing %s", line, want)
		}
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
	attrs := spanAttrs(t, s.spans)
	if got := attrs["search.outcome"].AsString(); got != "exact" {
		t.Fatalf("search.outcome = %q, want exact", got)
	}
	if got := attrs["search.ended_in"].AsString(); got != "probe" {
		t.Errorf("search.ended_in = %q, want probe", got)
	}
	if !attrs["search.progressed"].AsBool() {
		t.Error("search.progressed = false on a complete answer, want true")
	}
}

// Every timeout scales with the budget. At half the default the tail cap is
// half of 400 ms and the continuation cap half of 440 ms. A probe of 100 ms
// predicts a 250 ms step, over the 220 ms cap, so no step runs and the tail
// cap is what binds the tail's timeout.
func TestSearchScalesTailTimeoutWithBudget(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 500*time.Microsecond, func(i int) bool { return i == 15000 })
	corpus.tailCost = func(int) time.Duration { return 150 * ms }
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	page, fdb, _, err := runOn(t, fdb, clock, 400*ms, 50, 0, querycontract.SearchCursor{})
	if err != nil || page.Partial != nil {
		t.Fatalf("Search() = (%+v, %v), want an exact page", page, err)
	}
	if got := fdb.tx.labels(t, "step", "tail"); len(got) != 2 || got[1] != "tail" {
		t.Fatalf("statements = %v, want the probe then the tail: the predicted step is over the scaled cap", got)
	}
	assertTailShape(t, fdb.tx, "200ms")
	if got := timeouts(fdb.tx); len(got) == 0 || got[0] != "400ms" {
		t.Fatalf("statement timeouts = %v, want the probe's to be the whole 400 ms budget", got)
	}

	// With cheap rows the scaled cap bounds the continuation instead: 25 ms
	// steps fit eight times into 220 ms, and the phase-one cap is reported.
	s := newSignals(t, newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 }))
	s.searcher.Budget = 400 * ms
	if _, err := s.searcher.Search(context.Background(), "needle", 50, 0, querycontract.SearchCursor{}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	attrs := spanAttrs(t, s.spans)
	if got := attrs["search.phase_one_cap_ms"].AsInt64(); got != 220 {
		t.Errorf("search.phase_one_cap_ms = %d, want 220 at a 400 ms budget", got)
	}
	if got := attrs["search.continuation_steps"].AsInt64(); got != 8 {
		t.Errorf("search.continuation_steps = %d, want 8 (8 x 25 ms inside the 220 ms cap)", got)
	}
	// Each step's timeout is the room left in the 220 ms phase: the first is
	// the whole cap, and every later one shrinks by the time already used.
	got := timeouts(s.searcher.Store.(*fakeDB).tx)
	want := []string{"400ms", "220ms", "195ms", "170ms", "145ms", "120ms", "95ms", "70ms", "45ms"}
	if len(got) < len(want) || !equalKeys(got[:len(want)], want) {
		t.Errorf("statement timeouts = %v, want the probe at the budget then %v", got, want[1:])
	}
}

// timeouts lists the statement_timeout values the walk set, in order.
func timeouts(tx *fakeTx) []string {
	var out []string
	for i, label := range tx.log {
		if label == "set_timeout" {
			text, _ := tx.args[i][0].(string)
			out = append(out, text)
		}
	}
	return out
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
