// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryspan

import (
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// The span's identity is an operator contract: saved dashboard queries match on
// the tracer name and on these three attributes. doc.go and AGENTS.md both say
// so, but nothing asserted it, so renaming tracerName or dropping an attribute
// would compile and pass the whole suite while silently breaking every saved
// query built on the old name. This test turns that documented invariant into
// an enforced one.
func TestStartHandlerSpanWithEmitsTheOperatorContract(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	req := httptest.NewRequest("GET", "/api/v0/whatever", nil)
	_, span := StartHandlerSpanWith(
		provider.Tracer(tracerName), req, "query.example", "/api/v0/example", "example.capability",
	)
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	got := ended[0]

	if got.Name() != "query.example" {
		t.Errorf("span name = %q, want %q", got.Name(), "query.example")
	}
	if scope := got.InstrumentationScope().Name; scope != "eshu/go/internal/query" {
		t.Errorf("tracer name = %q, want %q — saved span queries and dashboards match on this", scope, "eshu/go/internal/query")
	}

	want := map[string]string{
		"http.route":        "/api/v0/example",
		"eshu.capability":   "example.capability",
		"service.namespace": "",
	}
	seen := map[string]string{}
	for _, attr := range got.Attributes() {
		seen[string(attr.Key)] = attr.Value.AsString()
	}
	for key, wantValue := range want {
		value, ok := seen[key]
		if !ok {
			t.Errorf("attribute %q missing; an operator dashboard filtering on it would return nothing", key)
			continue
		}
		if key == "service.namespace" {
			if value == "" {
				t.Errorf("attribute %q is empty, want the shared service namespace", key)
			}
			continue
		}
		if value != wantValue {
			t.Errorf("attribute %q = %q, want %q", key, value, wantValue)
		}
	}
}
