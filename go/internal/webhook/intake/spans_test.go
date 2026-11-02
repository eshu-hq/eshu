// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	spanHandle = "webhook.handle"
	spanStore  = "webhook.store"
)

// Closed attribute allow-lists for the two spans (#9815). They match the
// in-tree listener's spans, so no trigger id, delivery id, repository, account
// or scope id can become an attribute.
var (
	handleSpanKeys = []string{"decision", "event_kind", "outcome", "provider", "reason", "status"}
	storeSpanKeys  = []string{"decision", "event_kind", "outcome", "provider", "status"}
)

type spanHarness struct {
	recorder *tracetest.SpanRecorder
	tracer   trace.Tracer
}

func newSpanHarness() *spanHarness {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	return &spanHarness{recorder: recorder, tracer: provider.Tracer("intake-span-test")}
}

func (h *spanHarness) server(stores Stores) *Server {
	return NewServer(ServerConfig{Token: "core-intake-token", Tracer: h.tracer}, stores)
}

// named returns the ended spans with the given name.
func (h *spanHarness) named(name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, span := range h.recorder.Ended() {
		if span.Name() == name {
			out = append(out, span)
		}
	}
	return out
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]string {
	out := make(map[string]string)
	for _, kv := range span.Attributes() {
		out[string(kv.Key)] = kv.Value.AsString()
	}
	return out
}

func keysOf(attrs map[string]string) []string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// storeContext returns the context the route's fake store last received.
func (s *testStoreSet) storeContext(route string) context.Context {
	switch route {
	case "repository":
		return s.repository.ctx
	case "incident":
		return s.incident.ctx
	case "aws":
		return s.aws.ctx
	case "gcp":
		return s.gcp.ctx
	default:
		return s.actions.ctx
	}
}

// onlySpan fails the test unless exactly one span carries the name.
func onlySpan(t *testing.T, h *spanHarness, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := h.named(name)
	if len(spans) != 1 {
		t.Fatalf("%s spans = %d, want 1", name, len(spans))
	}
	return spans[0]
}

// TestIntakeStoredRequestEmitsNestedSpans is the #9815 red/green pair: every
// intake route emits one webhook.handle span with one webhook.store child, the
// store call receives the store span's context so the stores' own spans nest
// under it, and the attribute keys are exactly the closed allow-list.
func TestIntakeStoredRequestEmitsNestedSpans(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			h := newSpanHarness()
			stores := testStores()
			mux := h.server(stores.stores()).Mux()

			if rec := postIntake(t, mux, route.path, route.payload); rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d (%s), want 202", rec.Code, rec.Body.String())
			}

			handle := onlySpan(t, h, spanHandle)
			store := onlySpan(t, h, spanStore)
			if store.Parent().SpanID() != handle.SpanContext().SpanID() {
				t.Fatalf("store span parent = %s, want the handle span %s", store.Parent().SpanID(), handle.SpanContext().SpanID())
			}
			if got := trace.SpanContextFromContext(stores.storeContext(route.name)).SpanID(); got != store.SpanContext().SpanID() {
				t.Fatalf("store call span = %s, want the store span %s (children must nest under it)", got, store.SpanContext().SpanID())
			}

			handleAttrs, storeAttrs := spanAttrs(handle), spanAttrs(store)
			if got := keysOf(handleAttrs); !sameKeys(got, handleSpanKeys) {
				t.Fatalf("handle attribute keys = %v, want %v", got, handleSpanKeys)
			}
			if got := keysOf(storeAttrs); !sameKeys(got, storeSpanKeys) {
				t.Fatalf("store attribute keys = %v, want %v", got, storeSpanKeys)
			}
			for key, want := range map[string]string{
				"provider": route.provider, "event_kind": route.eventKind, "outcome": "stored", "reason": "none",
			} {
				if handleAttrs[key] != want {
					t.Fatalf("handle %s = %q, want %q", key, handleAttrs[key], want)
				}
			}
			for key, want := range map[string]string{
				"provider": route.provider, "event_kind": route.eventKind, "outcome": "stored",
			} {
				if storeAttrs[key] != want {
					t.Fatalf("store %s = %q, want %q", key, storeAttrs[key], want)
				}
			}
			if handle.Status().Code == codes.Error || store.Status().Code == codes.Error {
				t.Fatal("a stored request must not set span status Error")
			}
		})
	}
}

// TestIntakeRejectedRequestEmitsHandleSpanOnly pins that a request that never
// reached storage has a handle span with the rejection reason and no store span.
func TestIntakeRejectedRequestEmitsHandleSpanOnly(t *testing.T) {
	t.Parallel()

	route := intakeRoutes()[0]
	h := newSpanHarness()
	mux := h.server(testStores().stores()).Mux()

	rec := rawIntake(mux, http.MethodPost, route.path, "Bearer nope", marshalRoute(t, route.payload))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("intake = %d, want 401", rec.Code)
	}

	handle := onlySpan(t, h, spanHandle)
	attrs := spanAttrs(handle)
	if attrs["outcome"] != "rejected" || attrs["reason"] != "auth_failed" {
		t.Fatalf("handle outcome/reason = %q/%q, want rejected/auth_failed", attrs["outcome"], attrs["reason"])
	}
	if got := keysOf(attrs); !sameKeys(got, handleSpanKeys) {
		t.Fatalf("handle attribute keys = %v, want %v", got, handleSpanKeys)
	}
	if got := len(h.named(spanStore)); got != 0 {
		t.Fatalf("store spans = %d, want 0 for a rejected request", got)
	}
}

// TestIntakeStoreFailureMarksSpans pins the failure shape: the store span
// reads outcome and status failed, both spans carry status Error with a fixed
// description, and the raw driver error is never recorded.
func TestIntakeStoreFailureMarksSpans(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			h := newSpanHarness()
			stores := testStores()
			route.failStore(stores)
			mux := h.server(stores.stores()).Mux()

			if rec := postIntake(t, mux, route.path, route.payload); rec.Code != http.StatusInternalServerError {
				t.Fatalf("store failure = %d, want 500", rec.Code)
			}

			store := onlySpan(t, h, spanStore)
			storeAttrs := spanAttrs(store)
			if storeAttrs["outcome"] != "failed" || storeAttrs["status"] != "failed" {
				t.Fatalf("store outcome/status = %q/%q, want failed/failed", storeAttrs["outcome"], storeAttrs["status"])
			}
			handle := onlySpan(t, h, spanHandle)
			if got := spanAttrs(handle); got["outcome"] != "failed" || got["reason"] != "store_failed" {
				t.Fatalf("handle outcome/reason = %q/%q, want failed/store_failed", got["outcome"], got["reason"])
			}
			for _, span := range []sdktrace.ReadOnlySpan{store, handle} {
				if span.Status().Code != codes.Error || span.Status().Description != "store failed" {
					t.Fatalf("%s status = %v %q, want Error \"store failed\"", span.Name(), span.Status().Code, span.Status().Description)
				}
				if len(span.Events()) != 0 {
					t.Fatalf("%s recorded %d events; the raw store error must not be recorded", span.Name(), len(span.Events()))
				}
			}
		})
	}
}

// TestIntakeUnconfiguredStoreEmitsHandleSpanOnly pins that a route with no
// store has a failed handle span but no store span: no store call ran.
func TestIntakeUnconfiguredStoreEmitsHandleSpanOnly(t *testing.T) {
	t.Parallel()

	h := newSpanHarness()
	mux := h.server(Stores{}).Mux()
	route := intakeRoutes()[2]

	if rec := postIntake(t, mux, route.path, route.payload); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("intake = %d, want 503", rec.Code)
	}
	if got := spanAttrs(onlySpan(t, h, spanHandle))["outcome"]; got != "failed" {
		t.Fatalf("handle outcome = %q, want failed", got)
	}
	if got := len(h.named(spanStore)); got != 0 {
		t.Fatalf("store spans = %d, want 0", got)
	}
}

// TestIntakeActionsConflictMarksStoreSpanRejected pins the conflict path: the
// store worked and refused the delivery, so the store span reads rejected and
// is not an error.
func TestIntakeActionsConflictMarksStoreSpanRejected(t *testing.T) {
	t.Parallel()

	h := newSpanHarness()
	stores := testStores()
	stores.actions.err = webhook.ErrActionsDeliveryConflict
	mux := h.server(stores.stores()).Mux()
	route := intakeRoutes()[4]

	if rec := postIntake(t, mux, route.path, route.payload); rec.Code != http.StatusConflict {
		t.Fatalf("intake = %d, want 409", rec.Code)
	}
	store := onlySpan(t, h, spanStore)
	if got := spanAttrs(store)["outcome"]; got != "rejected" {
		t.Fatalf("store outcome = %q, want rejected", got)
	}
	if store.Status().Code == codes.Error {
		t.Fatal("a refused conflict must not set span status Error")
	}
}

// TestIntakeWithoutTracerEmitsNoSpans pins that a nil Tracer disables spans
// and still serves, and that /healthz never emits one.
func TestIntakeWithoutTracerEmitsNoSpans(t *testing.T) {
	t.Parallel()

	h := newSpanHarness()
	untraced := NewServer(ServerConfig{Token: "core-intake-token"}, testStores().stores()).Mux()
	for _, route := range intakeRoutes() {
		if rec := postIntake(t, untraced, route.path, route.payload); rec.Code != http.StatusAccepted {
			t.Fatalf("%s = %d, want 202", route.name, rec.Code)
		}
	}
	traced := h.server(testStores().stores()).Mux()
	if rec := rawIntake(traced, http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
	if got := len(h.recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}
