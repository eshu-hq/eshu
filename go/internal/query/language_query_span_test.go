// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestHandleLanguageQueryEmitsLanguageQuerySpan is the #5761 F7/P3-3 review-fix
// regression: handleLanguageQuery calls startQueryHandlerSpan so latency for
// this route is attributable to symbol_graph.language_entities rather than
// only to the reader layer, but nothing asserted the span was ever actually
// emitted -- deleting the startQueryHandlerSpan/defer span.End() call left the
// whole suite green. This swaps queryHandlerTracer for a recording provider
// (mirroring TestGraphEntityInventoryRecordsBoundedOutcomeTelemetry in
// graph_entity_inventory_counts_test.go) and asserts the handler emits
// exactly one span named telemetry.SpanQueryLanguageQuery carrying the route
// and capability attributes.
func TestHandleLanguageQueryEmitsLanguageQuerySpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("language-query-span-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{"entity_id": "e1", "name": "Foo"},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"Foo"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d (handleLanguageQuery must emit exactly one span)", got, want)
	}
	if got, want := spans[0].Name(), telemetry.SpanQueryLanguageQuery; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	if got, want := attributes["http.route"], "POST /api/v0/code/language-query"; got != want {
		t.Fatalf("span attribute http.route = %#v, want %#v", got, want)
	}
	// languageQueryCapability is an unexported family constant; asserted here
	// by its wire value (languageQueryCapabilityWire, shared with the sibling
	// capability assertions in language_query_graph_error_test.go) rather
	// than by reference.
	if got, want := attributes["eshu.capability"], languageQueryCapabilityWire; got != want {
		t.Fatalf("span attribute eshu.capability = %#v, want %#v", got, want)
	}
}
