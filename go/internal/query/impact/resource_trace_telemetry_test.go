// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestTraceResourceToCodeTraversalSpanCarriesStageAttributes is the #7931
// option-C regression: the trace-resource-to-code traversal stage emits a
// span carrying the closed stage-timing attribute set, and observes the
// graph-stage duration histogram, without changing the traversal statement
// or the response. Not parallel: it swaps queryHandlerTracer.
func TestTraceResourceToCodeTraversalSpanCarriesStageAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = traceProvider.Shutdown(context.Background()) })
	previous := queryHandlerTracer
	queryHandlerTracer = traceProvider.Tracer("impact-resource-trace-telemetry-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	metricReader := sdkmetric.NewManualReader()
	metricProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	t.Cleanup(func() { _ = metricProvider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(metricProvider.Meter("impact-resource-trace-telemetry-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	var gotCypher string
	traversalCalls := 0
	handler := &Handler{
		Profile:     querycontract.ProfileLocalAuthoritative,
		Instruments: instruments,
		Neo4j: graph.FakeGraphReaderWithSingle{
			RunSingleFn: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				traversalCalls++
				gotCypher = cypher
				if got, want := params["limit"], 11; got != want {
					t.Errorf("params[limit] = %#v, want %#v (requested 10 + 1)", got, want)
				}
				return []map[string]any{
					{"repo_id": "repo-a", "repo_name": "api", "depth": int64(1), "rels": []any{}},
					{"repo_id": "repo-b", "repo_name": "worker", "depth": int64(3), "rels": []any{}},
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-resource-to-code",
		bytes.NewBufferString(`{"start":"resource:queue","max_depth":4,"limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := testutil.DecodeImpactEnvelopeData(t, rec)
	paths, ok := data["paths"].([]any)
	if !ok || len(paths) != 2 {
		t.Fatalf("paths = %#v, want two rows", data["paths"])
	}
	if traversalCalls != 1 {
		t.Fatalf("traversal calls = %d, want 1 (observability must not add graph reads)", traversalCalls)
	}

	// The traversal statement itself is unchanged: byte-identical to the
	// deployment template formatted with the resolved anchor and depth.
	wantCypher := fmt.Sprintf(deployment.ImpactRepoPathCypher, "(start:CloudResource {id: $start_id})", 4)
	if gotCypher != wantCypher {
		t.Fatalf("traversal cypher mismatch:\n got: %q\nwant: %q", gotCypher, wantCypher)
	}

	span := findEndedSpan(t, recorder.Ended(), "query.resource_to_code.traversal")
	attrs := spanAttributes(span)
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.start_label", "CloudResource")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.max_depth", "4")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.limit", "10")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.scoped", "false")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.rows_expanded", "2")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.rows_returned", "2")
	assertSpanAttr(t, attrs, "eshu.query.resource_to_code.max_depth_reached", "3")
	if _, ok := attrs["eshu.query.resource_to_code.traversal_ms"]; !ok {
		t.Errorf("span has no eshu.query.resource_to_code.traversal_ms attribute: %v", attrs)
	}

	assertTraversalHistogram(t, metricReader, map[string]string{
		"route":      ownership.RouteTraceResourceToCode,
		"node_label": "CloudResource",
		"outcome":    "ok",
	})
}

func findEndedSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range ended {
		if span.Name() == name {
			return span
		}
	}
	names := make([]string, 0, len(ended))
	for _, span := range ended {
		names = append(names, span.Name())
	}
	t.Fatalf("no ended span named %q; ended spans = %v", name, names)
	return nil
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[string]string {
	out := map[string]string{}
	for _, kv := range span.Attributes() {
		// String formats every attribute type (string, int64, bool,
		// float64); AsString only reads STRING values.
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func assertSpanAttr(t *testing.T, attrs map[string]string, key, want string) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Errorf("span has no %q attribute: %v", key, attrs)
		return
	}
	if got != want {
		t.Errorf("span attribute %q = %q, want %q", key, got, want)
	}
}

// assertTraversalHistogram collects eshu_dp_query_resource_to_code_traversal_duration_seconds
// and requires exactly one datapoint with the closed label set.
func assertTraversalHistogram(t *testing.T, reader *sdkmetric.ManualReader, wantLabels map[string]string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	found := 0
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_query_resource_to_code_traversal_duration_seconds" {
				continue
			}
			hist, ok := record.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("histogram data = %T, want Histogram[float64]", record.Data)
			}
			for _, point := range hist.DataPoints {
				labels := map[string]string{}
				for _, kv := range point.Attributes.ToSlice() {
					labels[string(kv.Key)] = kv.Value.AsString()
				}
				match := true
				for key, want := range wantLabels {
					if labels[key] != want {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				found++
				if point.Count != 1 {
					t.Errorf("histogram count = %d, want 1", point.Count)
				}
			}
		}
	}
	if found != 1 {
		t.Fatalf("histogram datapoints with labels %v = %d, want 1", wantLabels, found)
	}
}
