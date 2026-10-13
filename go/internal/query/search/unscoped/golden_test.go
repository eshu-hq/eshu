// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/<route>_route.golden.json from the live searcher output")

// goldenStatement is one statement the transaction saw: the id of its exact
// text in goldenFile.SQL and its arguments, with the pgx exec mode spelled
// out.
type goldenStatement struct {
	SQL  string
	Args []string
}

// line renders the statement as one golden line: the text id, then each
// argument as type:value.
func (s goldenStatement) line() string {
	return s.SQL + " | " + strings.Join(s.Args, " | ")
}

// goldenFile is the committed golden of one route: the route's exported
// statement set, every distinct statement text the cases sent once (keyed by
// an id in order of first use), then one record per case. The statement set
// is pinned on its own because no fake case reaches the tail from an empty
// cursor: the probe's timeout is the whole remaining budget, so a cancelled
// probe leaves no room for a tail.
type goldenFile struct {
	StatementSet any               `json:"statement_set"`
	SQL          map[string]string `json:"sql"`
	Cases        []goldenRecord    `json:"cases"`
}

// goldenMetric is one data point of one instrument.
type goldenMetric struct {
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Value string            `json:"value"`
}

// goldenRecord is the full observable output of one case. Exactly one of
// Files and Entities is set, by the route that ran the case.
type goldenRecord struct {
	Name       string            `json:"name"`
	Files      *[]string         `json:"files,omitempty"`
	Entities   *[]string         `json:"entities,omitempty"`
	More       bool              `json:"more"`
	Partial    json.RawMessage   `json:"partial"`
	Statements []string          `json:"statements"`
	SpanName   string            `json:"span_name"`
	SpanAttrs  map[string]string `json:"span_attrs"`
	Metrics    []goldenMetric    `json:"metrics"`
	Logs       []map[string]any  `json:"logs"`
}

// goldenResult is what one route call returned, with each page row already
// rendered as its JSON.
type goldenResult struct {
	rows    []string
	more    bool
	partial *querycontract.SearchPartial
}

// goldenRoute is one search route whose output a golden pins: its golden
// file, its exported statement set, its cases, and the exported call that
// runs one case.
type goldenRoute struct {
	name         string
	path         string
	statementSet any
	cases        []goldenCase
	entities     bool
	search       func(context.Context, *unscoped.Searcher, goldenCase) (goldenResult, error)
}

// The file route's output is pinned byte for byte: the page rows, the partial
// object as the handler serializes it (field names, cursor encoding, reason
// and hint strings), every statement text and argument in order, the span,
// the metrics and the partial log line. A refactor of the engine must leave
// every byte of this record unchanged. Rewrite it only for an intended
// contract change, with -update-golden.
func TestFileRouteGolden(t *testing.T) {
	checkGolden(t, fileGoldenRoute())
}

// The entity route's output is pinned the same way, with its own cases for
// the four-part cursor: two entities that share a file, a start_line past the
// int32 column range, and a start_line of zero that the wire omits.
func TestEntityRouteGolden(t *testing.T) {
	checkGolden(t, entityGoldenRoute())
}

func checkGolden(t *testing.T, route goldenRoute) {
	t.Helper()
	file := goldenFile{StatementSet: route.statementSet, SQL: map[string]string{}}
	ids := map[string]string{}
	for _, tc := range route.cases {
		record, statements := recordGoldenCase(t, route, tc)
		for _, statement := range statements {
			id, ok := ids[statement.SQL]
			if !ok {
				id = fmt.Sprintf("s%02d", len(ids)+1)
				ids[statement.SQL] = id
				file.SQL[id] = statement.SQL
			}
			statement.SQL = id
			record.Statements = append(record.Statements, statement.line())
		}
		file.Cases = append(file.Cases, record)
	}
	got, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')
	if *updateGolden {
		if err := os.WriteFile(route.path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(route.path)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update-golden): %v", err)
	}
	if !bytes.Equal(got, want) {
		at := firstDiff(got, want)
		t.Fatalf("%s route output differs from %s at byte %d\n got: %s\nwant: %s",
			route.name, route.path, at, around(got, at), around(want, at))
	}
}

func recordGoldenCase(t *testing.T, route goldenRoute, tc goldenCase) (goldenRecord, []goldenStatement) {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("golden"))
	if err != nil {
		t.Fatalf("%s: NewInstruments() error = %v", tc.name, err)
	}
	logs := &bytes.Buffer{}
	clock := newFakeClock()
	store := &recordingStore{inner: newFakeDB(tc.corpus(), clock)}
	searcher := &unscoped.Searcher{
		Store:       store,
		Budget:      tc.budget,
		Now:         clock.Now,
		Tracer:      provider.Tracer("golden"),
		Instruments: instruments,
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.Attr{}
				}
				return a
			},
		})),
	}
	result, err := route.search(context.Background(), searcher, tc)
	if err != nil {
		t.Fatalf("%s: %s search error = %v", tc.name, route.name, err)
	}
	partial, err := json.Marshal(result.partial)
	if err != nil {
		t.Fatalf("%s: marshal partial: %v", tc.name, err)
	}
	rows := append([]string{}, result.rows...)
	record := goldenRecord{
		Name:       tc.name,
		More:       result.more,
		Partial:    partial,
		Statements: []string{},
		SpanAttrs:  map[string]string{},
		Metrics:    collectGoldenMetrics(t, reader),
		Logs:       []map[string]any{},
	}
	if route.entities {
		record.Entities = &rows
	} else {
		record.Files = &rows
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("%s: ended spans = %d, want 1", tc.name, len(ended))
	}
	record.SpanName = ended[0].Name()
	for _, kv := range ended[0].Attributes() {
		record.SpanAttrs[string(kv.Key)] = kv.Value.String()
	}
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		entry := map[string]any{}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("%s: log line %q: %v", tc.name, line, err)
		}
		record.Logs = append(record.Logs, entry)
	}
	return record, store.statements
}

// marshalRows renders each page row as its JSON.
func marshalRows[R any](rows []R) ([]string, error) {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		out = append(out, string(encoded))
	}
	return out, nil
}

func collectGoldenMetrics(t *testing.T, reader *sdkmetric.ManualReader) []goldenMetric {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := []goldenMetric{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					out = append(out, goldenMetric{Name: m.Name, Attrs: goldenAttrs(dp.Attributes.ToSlice()), Value: fmt.Sprint(dp.Value)})
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					out = append(out, goldenMetric{
						Name: m.Name, Attrs: goldenAttrs(dp.Attributes.ToSlice()),
						Value: fmt.Sprintf("count=%d sum=%g", dp.Count, dp.Sum),
					})
				}
			default:
				out = append(out, goldenMetric{Name: m.Name, Value: fmt.Sprintf("unhandled %T", data)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return fmt.Sprint(out[i].Attrs) < fmt.Sprint(out[j].Attrs)
	})
	return out
}

func goldenAttrs(kvs []attribute.KeyValue) map[string]string {
	if len(kvs) == 0 {
		return nil
	}
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

// recordingStore wraps the fake database and records every statement's exact
// text and arguments, so the golden pins the SQL the engine sends.
type recordingStore struct {
	inner      *fakeDB
	statements []goldenStatement
}

func (s *recordingStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	tx, err := s.inner.BeginReadOnlySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &recordingTx{ReadTransaction: tx, store: s}, nil
}

type recordingTx struct {
	db.ReadTransaction
	store *recordingStore
}

func (tx *recordingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	statement := goldenStatement{SQL: query, Args: []string{}}
	for _, arg := range args {
		if mode, ok := arg.(pgx.QueryExecMode); ok {
			statement.Args = append(statement.Args, "execMode="+mode.String())
			continue
		}
		statement.Args = append(statement.Args, fmt.Sprintf("%T:%v", arg, arg))
	}
	tx.store.statements = append(tx.store.statements, statement)
	return tx.ReadTransaction.QueryContext(ctx, query, args...)
}

func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func around(b []byte, at int) string {
	from := max(0, at-80)
	to := min(len(b), at+80)
	return string(b[from:to])
}
