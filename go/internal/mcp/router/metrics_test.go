// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// routerTelemetryForTest installs manual-reader and span-recorder providers
// globally. Tests using it must not call t.Parallel: the top-level parallel
// tests stay paused while sequential tests run, so nothing else records.
func routerTelemetryForTest(t *testing.T) (*sdkmetric.ManualReader, *tracetest.SpanRecorder) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousMeter, previousTracer := otel.GetMeterProvider(), otel.GetTracerProvider()
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)
	resetRouterMetricsForTest()
	t.Cleanup(func() {
		resetRouterMetricsForTest()
		otel.SetMeterProvider(previousMeter)
		otel.SetTracerProvider(previousTracer)
		_ = meterProvider.Shutdown(context.Background())
		_ = tracerProvider.Shutdown(context.Background())
	})
	return reader, recorder
}

type routerSample struct {
	finds map[string]int64
	calls map[[2]string]int64
	ranks []uint64
}

func collectRouterMetrics(t *testing.T, reader *sdkmetric.ManualReader) routerSample {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	s := routerSample{finds: map[string]int64{}, calls: map[[2]string]int64{}}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					outcome, _ := dp.Attributes.Value("outcome")
					tool, _ := dp.Attributes.Value("tool")
					switch m.Name {
					case "eshu_dp_mcp_router_find_total":
						s.finds[outcome.AsString()] += dp.Value
					case "eshu_dp_mcp_router_call_total":
						s.calls[[2]string{tool.AsString(), outcome.AsString()}] += dp.Value
					}
				}
			case metricdata.Histogram[int64]:
				if m.Name == "eshu_dp_mcp_router_target_rank" {
					for _, dp := range data.DataPoints {
						s.ranks = dp.BucketCounts
					}
				}
			}
		}
	}
	return s
}

func TestRouterTelemetryRecordsOutcomesAndTargetRank(t *testing.T) {
	reader, recorder := routerTelemetryForTest(t)
	engine := newFixtureEngine(t, orderRanker{})
	d := &recordingDispatcher{}
	req := fixtureRequest(d)
	ctx := context.Background()

	engine.Find(ctx, req, map[string]any{"intent": "x", "limit": 3}) // candidates: find_code, execute_cypher_query, get_repo_summary
	engine.Find(ctx, req, map[string]any{})                          // error
	engine.Call(ctx, req, map[string]any{"name": "get_repo_summary", "arguments": map[string]any{}})
	engine.Call(ctx, req, map[string]any{"name": "find_code", "arguments": map[string]any{}}) // invalid
	engine.Call(ctx, req, map[string]any{"name": "nope_tool", "arguments": map[string]any{}}) // unknown
	engine.Call(ctx, req, map[string]any{"name": "call_tool", "arguments": map[string]any{}}) // refused
	engine.Call(ctx, req, map[string]any{"name": "build_evidence_citation_packet", "arguments": map[string]any{}})

	got := collectRouterMetrics(t, reader)
	if got.finds["candidates"] != 1 || got.finds["error"] != 1 {
		t.Fatalf("find outcomes = %v, want candidates=1 error=1", got.finds)
	}
	for key, want := range map[[2]string]int64{
		{"get_repo_summary", "dispatched"}:               1,
		{"find_code", "invalid_arguments"}:               1,
		{"unregistered", "unknown_tool"}:                 1,
		{"call_tool", "refused"}:                         1,
		{"build_evidence_citation_packet", "dispatched"}: 1,
	} {
		if got.calls[key] != want {
			t.Fatalf("call %v = %d, want %d (all: %v)", key, got.calls[key], want, got.calls)
		}
	}
	// Buckets: [<=0, <=1, <=2, <=3, ...]. Only the two dispatched calls record:
	// get_repo_summary sat third (rank 3) and build_evidence_citation_packet
	// was not offered (0). The invalid find_code call records nothing.
	if len(got.ranks) < 4 || got.ranks[0] != 1 || got.ranks[1] != 0 || got.ranks[3] != 1 || sumBuckets(got.ranks) != 2 {
		t.Fatalf("rank buckets = %v, want one each at 0 and 3 and nothing else", got.ranks)
	}

	var sawFind, sawCall bool
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case contract.SpanMCPRouterFind:
			sawFind = true
		case contract.SpanMCPRouterCall:
			for _, attr := range span.Attributes() {
				if attr.Key == "mcp.router.target" && attr.Value.AsString() == "get_repo_summary" {
					sawCall = true
				}
			}
		}
	}
	if !sawFind || !sawCall {
		t.Fatalf("spans: find=%v call-with-target=%v, want both", sawFind, sawCall)
	}
}

func TestRecentFindsIsBoundedAndPerSession(t *testing.T) {
	t.Parallel()

	r := newRecentFinds(2)
	r.remember("a", []string{"x", "y"})
	r.remember("b", []string{"y"})
	if got := r.rank("a", "y"); got != 2 {
		t.Fatalf("rank(a, y) = %d, want 2", got)
	}
	if got := r.rank("b", "x"); got != 0 {
		t.Fatalf("rank(b, x) = %d, want 0", got)
	}
	r.remember("c", []string{"z"})
	if got := r.rank("a", "x"); got != 0 {
		t.Fatalf("rank(a, x) after eviction = %d, want 0", got)
	}
	r.remember("b", []string{"q"})
	if got := r.rank("b", "q"); got != 1 {
		t.Fatalf("rank(b, q) after refresh = %d, want 1", got)
	}
}

// TestTargetRankRecordsDispatchedCallsOnly keeps the rank histogram readable:
// a refused or malformed call says nothing about whether ranking matched the
// agent's intent, so only dispatched calls record a rank.
func TestTargetRankRecordsDispatchedCallsOnly(t *testing.T) {
	reader, _ := routerTelemetryForTest(t)
	engine := newFixtureEngine(t, orderRanker{})
	req := fixtureRequest(&recordingDispatcher{})

	engine.Find(context.Background(), req, map[string]any{"intent": "x", "limit": 3})
	engine.Call(context.Background(), req, map[string]any{"name": "find_code", "arguments": map[string]any{}})                                 // invalid_arguments
	engine.Call(context.Background(), req, map[string]any{"name": "ask", "arguments": map[string]any{"question": "q"}})                        // refused, registered target
	engine.Call(scopedContext(), req, map[string]any{"name": "execute_cypher_query", "arguments": map[string]any{"cypher_query": "RETURN 1"}}) // refused, not permitted
	if ranks := collectRouterMetrics(t, reader).ranks; sumBuckets(ranks) != 0 {
		t.Fatalf("rank buckets after refused and invalid calls = %v, want no samples", ranks)
	}
	engine.Call(context.Background(), req, map[string]any{"name": "find_code", "arguments": map[string]any{"query": "handler"}})
	ranks := collectRouterMetrics(t, reader).ranks
	if sumBuckets(ranks) != 1 || len(ranks) < 2 || ranks[1] != 1 {
		t.Fatalf("rank buckets after one dispatched call = %v, want one sample at rank 1", ranks)
	}
}

func sumBuckets(buckets []uint64) uint64 {
	var total uint64
	for _, n := range buckets {
		total += n
	}
	return total
}
