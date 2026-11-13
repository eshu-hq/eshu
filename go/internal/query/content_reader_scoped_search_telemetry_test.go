// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestScopedSearchPatternClass pins the closed pattern-class vocabulary and
// its precedence: short (<3 runes) first, then wildcard (%, _, \), then the
// shared trigram walk classification, else trigram. The classes must never
// carry the pattern text, only the class name.
func TestScopedSearchPatternClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern string
		want    string
	}{
		{"", "short"},
		{"ab", "short"},
		{"a%", "short"}, // short wins even with a wildcard rune present
		{"ab%", "wildcard"},
		{"abc_def", "wildcard"},
		{`abc\def`, "wildcard"},
		{"%", "short"},
		{"%_%", "wildcard"},
		{"abc", "trigram"},
		{"ab!cde", "trigram"}, // run of 3 alphanumerics survives the break
		{"needle-secret-pattern", "trigram"},
		{"αβγ", "trigram"}, // unicode letters count for the trigram walk
		{"a!b!c", "no_trigram"},
		{"!!!", "no_trigram"},
		{"ab!c", "no_trigram"}, // short alphanumeric runs only: the dense-tail class
		{"db_", "wildcard"},    // wildcard precedence beats the trigram walk
	}
	for _, tc := range cases {
		if got := scopedSearchPatternClass(tc.pattern); got != tc.want {
			t.Errorf("scopedSearchPatternClass(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

// TestScopedSearchScopeClass pins the operation-to-scope mapping: the _page
// operations read one repository, the _in_repos operations read a set.
func TestScopedSearchScopeClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		operation string
		want      string
	}{
		{"search_file_content_page", "single"},
		{"search_entity_content_page", "single"},
		{"search_file_content_in_repos", "multi"},
		{"search_entity_content_in_repos", "multi"},
	}
	for _, tc := range cases {
		if got := scopedSearchScopeClass(tc.operation); got != tc.want {
			t.Errorf("scopedSearchScopeClass(%q) = %q, want %q", tc.operation, got, tc.want)
		}
	}
}

// TestScopedSearchOutcome pins the exit classification: the context error
// decides deadline/cancel first so client aborts never inflate the deadline
// rate the threshold reads; any other error is error; nil is ok.
func TestScopedSearchOutcome(t *testing.T) {
	t.Parallel()

	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancelDeadline()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := scopedSearchOutcome(deadlineCtx, errors.New("boom")); got != "context_deadline" {
		t.Errorf("deadline ctx + err = %q, want context_deadline", got)
	}
	if got := scopedSearchOutcome(deadlineCtx, nil); got != "context_deadline" {
		t.Errorf("deadline ctx, nil err = %q, want context_deadline", got)
	}
	if got := scopedSearchOutcome(canceledCtx, errors.New("boom")); got != "context_canceled" {
		t.Errorf("canceled ctx + err = %q, want context_canceled", got)
	}
	if got := scopedSearchOutcome(context.Background(), errors.New("boom")); got != "error" {
		t.Errorf("live ctx + err = %q, want error", got)
	}
	if got := scopedSearchOutcome(context.Background(), nil); got != "ok" {
		t.Errorf("live ctx, nil err = %q, want ok", got)
	}
}

// scopedSignalHarness wires a real Instruments against a manual reader plus a
// span recorder, so one recordScopedSearch call is observable end to end.
type scopedSignalHarness struct {
	reader    *sdkmetric.ManualReader
	spans     *tracetest.SpanRecorder
	inst      *telemetry.Instruments
	logs      *bytes.Buffer
	restorePP func()
}

func newScopedSignalHarness(t *testing.T) *scopedSignalHarness {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	return &scopedSignalHarness{
		reader:    reader,
		spans:     spans,
		inst:      inst,
		logs:      logs,
		restorePP: func() { slog.SetDefault(prev) },
	}
}

// TestRecordScopedSearchEmitsOneCountAndOneSamplePerOutcome drives every
// outcome through recordScopedSearch and asserts exactly one counter
// increment and one histogram sample with the closed label set, the scoped
// span attributes, and a WARN log line only on context_deadline.
func TestRecordScopedSearchEmitsOneCountAndOneSamplePerOutcome(t *testing.T) {
	cases := []struct {
		name    string
		ctx     func() context.Context
		err     error
		outcome string
		wantLog bool
	}{
		{"ok", context.Background, nil, "ok", false},
		{"error", context.Background, errors.New("store down"), "error", false},
		{
			"deadline",
			func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
				// The deadline already fired; cancel releases the timer only.
				defer cancel()
				return ctx
			},
			errors.New("query failed"),
			"context_deadline",
			true,
		},
		{
			"canceled",
			func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			errors.New("query failed"),
			"context_canceled",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newScopedSignalHarness(t)
			t.Cleanup(h.restorePP)
			ctx := tc.ctx()
			_, span := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(h.spans)).Tracer("test").Start(ctx, "postgres.query")

			recordScopedSearch(ctx, h.inst, span, "search_file_content_page", "single", "trigram", time.Now().Add(-1250*time.Millisecond), 3, tc.err, 10, 0)
			span.End()

			counts := map[string]int64{}
			samples := map[string]uint64{}
			labels := map[string]map[string]string{}
			var collected metricdata.ResourceMetrics
			if err := h.reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch data := m.Data.(type) {
					case metricdata.Sum[int64]:
						for _, dp := range data.DataPoints {
							counts[m.Name] += dp.Value
							got := map[string]string{}
							for _, kv := range dp.Attributes.ToSlice() {
								got[string(kv.Key)] = kv.Value.AsString()
							}
							labels[m.Name] = got
						}
					case metricdata.Histogram[float64]:
						for _, dp := range data.DataPoints {
							samples[m.Name] += dp.Count
						}
					}
				}
			}
			if counts["eshu_dp_content_search_scoped_total"] != 1 {
				t.Errorf("scoped counter = %d, want 1", counts["eshu_dp_content_search_scoped_total"])
			}
			if samples["eshu_dp_content_search_scoped_duration_seconds"] != 1 {
				t.Errorf("scoped histogram samples = %d, want 1", samples["eshu_dp_content_search_scoped_duration_seconds"])
			}
			want := map[string]string{
				"operation":     "search_file_content_page",
				"scope_class":   "single",
				"pattern_class": "trigram",
				"outcome":       tc.outcome,
			}
			for k, w := range want {
				if labels["eshu_dp_content_search_scoped_total"][k] != w {
					t.Errorf("counter label %s = %q, want %q (all %v)", k, labels["eshu_dp_content_search_scoped_total"][k], w, labels["eshu_dp_content_search_scoped_total"])
				}
			}
			ended := h.spans.Ended()
			if len(ended) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(ended))
			}
			attrs := map[string]string{}
			for _, kv := range ended[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value.AsString()
			}
			if attrs["search.scope"] != "scoped" {
				t.Errorf("search.scope = %q, want scoped", attrs["search.scope"])
			}
			if attrs["search.pattern_class"] != "trigram" {
				t.Errorf("search.pattern_class = %q, want trigram", attrs["search.pattern_class"])
			}
			rowsReturned := int64(-1)
			for _, kv := range ended[0].Attributes() {
				if string(kv.Key) == "search.rows_returned" {
					rowsReturned = kv.Value.AsInt64()
				}
			}
			if rowsReturned != 3 {
				t.Errorf("search.rows_returned = %d, want 3", rowsReturned)
			}
			hasWarn := bytes.Contains(h.logs.Bytes(), []byte(`"level":"WARN"`))
			if hasWarn != tc.wantLog {
				t.Errorf("WARN log present = %v, want %v (log: %s)", hasWarn, tc.wantLog, h.logs.String())
			}
			if tc.wantLog && !bytes.Contains(h.logs.Bytes(), []byte("scoped_search_context_deadline")) {
				t.Errorf("deadline WARN log missing event name (log: %s)", h.logs.String())
			}
		})
	}
}

// scopedSearchFakeRows serves canned scan values through the db.Rows seam.
type scopedSearchFakeRows struct {
	rows [][]any
	pos  int
}

func (r *scopedSearchFakeRows) Next() bool {
	r.pos++
	return r.pos <= len(r.rows)
}

func (r *scopedSearchFakeRows) Scan(dest ...any) error {
	row := r.rows[r.pos-1]
	if len(dest) != len(row) {
		return errors.New("fake row width mismatch")
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = row[i].(string)
		case *int:
			*d = row[i].(int)
		case *[]byte:
			*d = row[i].([]byte)
		default:
			return errors.New("fake row cannot scan destination")
		}
	}
	return nil
}

func (r *scopedSearchFakeRows) Err() error   { return nil }
func (r *scopedSearchFakeRows) Close() error { return nil }

// scopedSearchFakeStore serves one canned result (or error) without Postgres.
type scopedSearchFakeStore struct {
	rows [][]any
	err  error
}

func (s *scopedSearchFakeStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &scopedSearchFakeRows{rows: s.rows}, nil
}

func (s *scopedSearchFakeStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return &scopedSearchFakeRow{err: errors.New("no rows")}
}

func (s *scopedSearchFakeStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshots unsupported in scoped-search fake")
}

// scopedSearchFakeRow is the single-row seam; the chokepoint never uses it.
type scopedSearchFakeRow struct{ err error }

func (r *scopedSearchFakeRow) Scan(...any) error { return r.err }

// TestSearchFileContentScopedRecordsOKThroughChokepoint proves the file
// chokepoint records one ok sample and returns the rows unchanged.
func TestSearchFileContentScopedRecordsOKThroughChokepoint(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	cr := NewContentReaderWithReadStore(&scopedSearchFakeStore{
		rows: [][]any{{"repo-a", "a/b.go", "sha", "needle here", "hash", 10, "go", ""}},
	}).WithInstruments(inst)

	results, err := cr.searchFileContentScoped(context.Background(), "search_file_content_page", "needle", fileContentSingleScopeWhere(1, 2), []any{"repo-a", "needle"}, 10, 0)
	if err != nil {
		t.Fatalf("searchFileContentScoped() error = %v", err)
	}
	if len(results) != 1 || results[0].Content != "needle here" {
		t.Fatalf("searchFileContentScoped() = %+v, want the canned row", results)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	found := false
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_content_search_scoped_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("scoped counter is %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				if dp.Value != 1 {
					t.Errorf("scoped counter datapoint = %d, want 1", dp.Value)
				}
				if v, ok := dp.Attributes.Value("outcome"); !ok || v.AsString() != "ok" {
					t.Errorf("outcome label = %v, want ok", dp.Attributes)
				}
				if v, ok := dp.Attributes.Value("pattern_class"); !ok || v.AsString() != "trigram" {
					t.Errorf("pattern_class label = %v, want trigram", dp.Attributes)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("eshu_dp_content_search_scoped_total has no datapoint after one ok call")
	}
}

// TestSearchEntityContentScopedRecordsErrorThroughChokepoint proves the entity
// chokepoint maps a store failure to the error outcome without changing the
// wrapped error the caller sees.
func TestSearchEntityContentScopedRecordsErrorThroughChokepoint(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	storeErr := errors.New("connection refused")
	cr := NewContentReaderWithReadStore(&scopedSearchFakeStore{err: storeErr}).WithInstruments(inst)

	_, err = cr.searchEntityContentScoped(context.Background(), "search_entity_content_in_repos", "needle", "repo_id = $1", []any{"a\x1fneedle"}, 10, 0, false)
	if !errors.Is(err, storeErr) {
		t.Fatalf("searchEntityContentScoped() error = %v, want wrapped %v", err, storeErr)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	foundOutcome := ""
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_content_search_scoped_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value("outcome"); ok {
					foundOutcome = v.AsString()
				}
			}
		}
	}
	if foundOutcome != "error" {
		t.Errorf("outcome label = %q, want error", foundOutcome)
	}
}
