// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// searchGraph records every statement and fails the full-text one on demand.
type searchGraph struct {
	ftErr   error
	ftRows  []map[string]any
	legRows []map[string]any
	calls   []string
	params  []map[string]any
}

func (g *searchGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, cypher)
	g.params = append(g.params, params)
	if strings.Contains(cypher, "db.index.fulltext.queryNodes") {
		return g.ftRows, g.ftErr
	}
	return g.legRows, nil
}

func readyReader(t *testing.T, inst *telemetry.Instruments) *fulltext.Reader {
	t.Helper()
	readiness := fulltext.NewReadiness(&probeGraph{state: "ONLINE"}, nil, nil)
	if !readiness.Refresh(context.Background()).Ready {
		t.Fatal("readiness not ready")
	}
	return &fulltext.Reader{Enabled: true, Readiness: readiness, Instruments: inst}
}

func baseRequest() fulltext.Request {
	return fulltext.Request{
		Neo4j:       true,
		Query:       "aws_iam_role",
		Labels:      []string{"A", "B"},
		WhereExtra:  "\n AND x",
		ReturnExprs: []string{"coalesce(n.name, '') as name"},
		Legacy:      "LEGACY",
		Params:      map[string]any{"limit": 51, "query": "aws_iam_role"},
	}
}

func newTelemetry(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return inst, reader
}

func pathCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_infra_search_path_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				path, _ := dp.Attributes.Value("search_path")
				reason, _ := dp.Attributes.Value("reason")
				out[path.AsString()+"/"+reason.AsString()] += dp.Value
			}
		}
	}
	return out
}

func spanAttrs(t *testing.T, run func(ctx context.Context)) map[string]string {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := tp.Tracer("test").Start(context.Background(), "search")
	run(ctx)
	span.End()
	out := map[string]string{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		out[string(kv.Key)] = kv.Value.AsString()
	}
	return out
}

func TestRowsUsesFulltextWhenReady(t *testing.T) {
	t.Parallel()
	inst, metrics := newTelemetry(t)
	graph := &searchGraph{ftRows: []map[string]any{{"name": "ft"}}}
	req := baseRequest()
	var rows []map[string]any
	attrs := spanAttrs(t, func(ctx context.Context) {
		var err error
		rows, err = readyReader(t, inst).Rows(ctx, graph, req)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(graph.calls) != 1 || !strings.Contains(graph.calls[0], "queryNodes") {
		t.Fatalf("calls = %q, want one full-text read", graph.calls)
	}
	if got := graph.params[0][fulltext.LuceneParam]; got != fulltext.Lucene("aws_iam_role") {
		t.Fatalf("lucene param = %v", got)
	}
	if _, leaked := req.Params[fulltext.LuceneParam]; leaked {
		t.Fatal("full-text param leaked into the caller's legacy params")
	}
	if rows[0]["name"] != "ft" {
		t.Fatalf("rows = %v, want full-text rows", rows)
	}
	if attrs["eshu.infra_search_path"] != "fulltext" {
		t.Fatalf("span attrs = %v", attrs)
	}
	if got := pathCounts(t, metrics); got["fulltext/ready"] != 1 || len(got) != 1 {
		t.Fatalf("path counts = %v", got)
	}
}

func TestRowsScopedUsesScopedStatement(t *testing.T) {
	t.Parallel()
	graph := &searchGraph{}
	req := baseRequest()
	req.Scoped = true
	req.ScopePredicate = "(GRANT)"
	if _, err := readyReader(t, nil).Rows(context.Background(), graph, req); err != nil {
		t.Fatal(err)
	}
	want := fulltext.ScopedStatement(req.Labels, req.WhereExtra, req.ScopePredicate, req.ReturnExprs)
	if graph.calls[0] != want {
		t.Fatalf("scoped statement = %s\nwant %s", graph.calls[0], want)
	}
}

func TestRowsFallsBackToLegacyOnFulltextError(t *testing.T) {
	t.Parallel()
	inst, metrics := newTelemetry(t)
	graph := &searchGraph{ftErr: errors.New("index failed"), legRows: []map[string]any{{"name": "legacy"}}}
	req := baseRequest()
	var rows []map[string]any
	attrs := spanAttrs(t, func(ctx context.Context) {
		var err error
		rows, err = readyReader(t, inst).Rows(ctx, graph, req)
		if err != nil {
			t.Fatalf("Rows() error = %v, want legacy fallback", err)
		}
	})
	if len(graph.calls) != 2 || graph.calls[1] != "LEGACY" {
		t.Fatalf("calls = %q, want full-text then legacy", graph.calls)
	}
	if _, ok := graph.params[1][fulltext.LuceneParam]; ok {
		t.Fatal("legacy fallback received the full-text param")
	}
	if len(rows) != 1 || rows[0]["name"] != "legacy" {
		t.Fatalf("rows = %v, want only legacy rows", rows)
	}
	if attrs["eshu.infra_search_path"] != "legacy" || attrs["eshu.infra_search_fallback_reason"] != "ft_error" {
		t.Fatalf("span attrs = %v", attrs)
	}
	if got := pathCounts(t, metrics); got["legacy/ft_error"] != 1 || len(got) != 1 {
		t.Fatalf("path counts = %v", got)
	}
}

func TestRowsDoesNotFallBackAfterCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	graph := &searchGraph{ftErr: context.Canceled}
	_, err := readyReader(t, nil).Rows(ctx, graph, baseRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Rows() error = %v, want context.Canceled", err)
	}
	if len(graph.calls) != 1 {
		t.Fatalf("calls = %q, want no legacy retry after cancel", graph.calls)
	}
}

func TestRowsLegacyPathsRunLegacyOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		reader func(t *testing.T, inst *telemetry.Instruments) *fulltext.Reader
		mutate func(*fulltext.Request)
		reason string
	}{
		{"nil reader", func(*testing.T, *telemetry.Instruments) *fulltext.Reader { return nil }, func(*fulltext.Request) {}, "flag_off"},
		{"flag off", func(t *testing.T, inst *telemetry.Instruments) *fulltext.Reader {
			r := readyReader(t, inst)
			r.Enabled = false
			return r
		}, func(*fulltext.Request) {}, "flag_off"},
		{"not ready", func(_ *testing.T, inst *telemetry.Instruments) *fulltext.Reader {
			return &fulltext.Reader{Enabled: true, Readiness: fulltext.NewReadiness(&probeGraph{}, nil, nil), Instruments: inst}
		}, func(*fulltext.Request) {}, "not_ready"},
		{"nornicdb", readyReader, func(r *fulltext.Request) { r.Neo4j = false }, "backend"},
		{"no query", readyReader, func(r *fulltext.Request) { r.Query = "" }, "no_query"},
	}
	for _, c := range cases {
		inst, metrics := newTelemetry(t)
		graph := &searchGraph{}
		req := baseRequest()
		c.mutate(&req)
		reader := c.reader(t, inst)
		if reader != nil {
			reader.Instruments = inst
		}
		if _, err := reader.Rows(context.Background(), graph, req); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(graph.calls) != 1 || graph.calls[0] != "LEGACY" {
			t.Fatalf("%s: calls = %q, want legacy only", c.name, graph.calls)
		}
		if reader != nil {
			if got := pathCounts(t, metrics); got["legacy/"+c.reason] != 1 {
				t.Fatalf("%s: path counts = %v", c.name, got)
			}
		}
	}
}
