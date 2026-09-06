// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestListOwnershipEmitsCodeownersOwnershipSpan is the copy-drift guard for
// this package's family-local startQueryHandlerSpan (see handler_tracing.go):
// the helper is the third copy of one trivial delegation (root's
// handler_tracing.go, supplychain/handler_tracing.go, and this file), and
// nothing but this test ties the copies together. It swaps
// queryHandlerTracer for a recording provider (mirroring root's
// TestHandleLanguageQueryEmitsLanguageQuerySpan) and asserts the ownership
// route emits exactly one span named telemetry.SpanQueryCodeownersOwnership
// carrying the route and capability attributes. A drifted copy -- a
// differently seeded tracer, a dropped attribute, a renamed span -- fails
// here rather than silently breaking the saved span queries and dashboards
// built on the `query.codeowners_ownership` name.
func TestListOwnershipEmitsCodeownersOwnershipSpan(t *testing.T) {
	// No t.Parallel: this test swaps the package-global queryHandlerTracer,
	// and a parallel sibling emitting its own handler span mid-swap would
	// land in this recorder (or miss it), flaking the exact-one-span
	// assertion. Root's language-query span test serializes the same way.
	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("codeowners-span-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	handler := &Handler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id=repo-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d (ListOwnership must emit exactly one span)", got, want)
	}
	if got, want := spans[0].Name(), telemetry.SpanQueryCodeownersOwnership; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	if got, want := attributes["http.route"], "GET /api/v0/codeowners/ownership"; got != want {
		t.Fatalf("span attribute http.route = %#v, want %#v", got, want)
	}
	if got, want := attributes["eshu.capability"], codeownersOwnershipCapability; got != want {
		t.Fatalf("span attribute eshu.capability = %#v, want %#v", got, want)
	}
}
