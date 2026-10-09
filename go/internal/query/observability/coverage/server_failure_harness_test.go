// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coverage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// serverFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const serverFailureCanary = "password=hunter2 MATCH (n)"

// serverFailureCase is one way a backend read fails: a server fault whose
// text carries the canary, a client cancel, or a stale PostgreSQL reader.
type serverFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func serverFailureCases() []serverFailureCase {
	return []serverFailureCase{
		{name: "backend failure", err: errors.New("backend: " + serverFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", serverFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", serverFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// serverFailureMounter is the route surface every handler in this package
// exposes.
type serverFailureMounter interface{ Mount(*http.ServeMux) }

// serverFailureRoute is one converted failure site: the request that reaches
// it, the handler whose read for that step fails with err, and the fixed
// message a server fault answers. method defaults to GET; scope, when set,
// decorates the request context (for example with a scoped grant).
type serverFailureRoute struct {
	name    string
	method  string
	path    string
	body    string
	message string
	handler func(err error) serverFailureMounter
	scope   func(context.Context) context.Context
}

// runServerFailureRoutes drives every route through every failure case. It
// swaps the package tracer, so callers must not be parallel.
func runServerFailureRoutes(t *testing.T, routes []serverFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range serverFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				rec, ended := serveServerFailure(t, route, route.handler(tc.err), tc.cancel)
				assertServerFailure(t, route.message, tc, rec, ended)
			})
		}
	}
}

// serveServerFailure serves one request under a recording tracer and returns
// the response and every ended span. cancel cancels the request context first.
func serveServerFailure(
	t *testing.T, route serverFailureRoute, handler serverFailureMounter, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := coverageHandlerTracer
	coverageHandlerTracer = provider.Tracer("server-failure-test")
	t.Cleanup(func() { coverageHandlerTracer = previous })

	ctx, parent := provider.Tracer("server-failure-test").Start(context.Background(), "request")
	if route.scope != nil {
		ctx = route.scope(ctx)
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	method := route.method
	if method == "" {
		method = http.MethodGet
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(method, route.path, strings.NewReader(route.body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// assertServerFailure checks one failure case: no canary in the body, the
// case's status, then the reader-fence verdict or the fixed message and the
// handler span's shape.
func assertServerFailure(
	t *testing.T, message string, tc serverFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan,
) {
	t.Helper()
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "MATCH (n)") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	wantStatus := http.StatusInternalServerError
	switch {
	case tc.cancel:
		wantStatus = querycontract.StatusClientClosedRequest
	case tc.stale:
		wantStatus = http.StatusServiceUnavailable
	}
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	if tc.stale {
		var envelope querycontract.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil ||
			envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("reader fence answer = %s (decode err %v, Retry-After %q), want backend_unavailable with Retry-After",
				body, err, rec.Header().Get("Retry-After"))
		}
		return
	}
	if got := serverFailureMessage(rec.Body.Bytes()); got != message {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, message, body)
	}
	assertServerFailureSpan(t, message, tc.cancel, ended)
}

// serverFailureMessage returns the text of an error body in either shape: the
// plain {"detail": ...} body or the {"error": {"message": ...}} envelope.
func serverFailureMessage(body []byte) string {
	var plain struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &plain) == nil && plain.Detail != "" {
		return plain.Detail
	}
	var envelope querycontract.ResponseEnvelope
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		return envelope.Error.Message
	}
	return ""
}

// assertServerFailureSpan checks the handler span: a client cancel leaves the
// status Unset with only the cancel event; a server fault sets Error with
// message as its description and records exactly one exception event.
func assertServerFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	var span sdktrace.ReadOnlySpan
	for _, candidate := range ended {
		if candidate.Name() != "request" {
			span = candidate
			break
		}
	}
	if span == nil {
		t.Fatal("no handler span recorded")
	}
	exceptions, hasCanceled := 0, false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			exceptions++
		}
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := span.Status()
	if cancel {
		if status.Code != codes.Unset || exceptions != 0 || !hasCanceled {
			t.Fatalf("cancel span status = %v, exceptions = %d, %s = %v; want Unset, 0, true",
				status.Code, exceptions, tracing.ClientCanceledEvent, hasCanceled)
		}
		return
	}
	if status.Code != codes.Error || status.Description != message || exceptions != 1 || hasCanceled {
		t.Fatalf("fault span status = %v (%q), exceptions = %d, canceled event = %v; want Error (%q), 1, false",
			status.Code, status.Description, exceptions, hasCanceled, message)
	}
}
