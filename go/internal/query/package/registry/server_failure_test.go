// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

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

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// registryFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const registryFailureCanary = "password=hunter2 MATCH (n)"

// registryFailureCase is one way a backend read fails: a server fault whose
// text carries the canary, a client cancel, or a stale PostgreSQL reader.
type registryFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func registryFailureCases() []registryFailureCase {
	return []registryFailureCase{
		{name: "backend failure", err: errors.New("backend: " + registryFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", registryFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", registryFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// registryFailureRoute is one converted failure step: the request that
// reaches it, whether the caller holds a scoped grant, the handler whose read
// for that step fails with err, and the fixed message a server fault answers.
type registryFailureRoute struct {
	name    string
	path    string
	scoped  bool
	message string
	handler func(err error) *Handler
}

// runRegistryFailureRoutes drives every route through every failure case. It
// swaps the package-level packageregTracer, so callers must not be parallel.
func runRegistryFailureRoutes(t *testing.T, routes []registryFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range registryFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				rec, ended := serveRegistryTraced(t, route.handler(tc.err), route.path, route.scoped, tc.cancel)
				assertRegistryFailure(t, route.message, tc, rec, ended)
			})
		}
	}
}

// serveRegistryTraced serves one GET under a recording tracer and returns the
// response and every ended span. scoped attaches tenant A's scoped grant;
// cancel cancels the request context first.
func serveRegistryTraced(
	t *testing.T, handler *Handler, path string, scoped, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := packageregTracer
	packageregTracer = provider.Tracer("registry-server-failure-test")
	t.Cleanup(func() { packageregTracer = previous })

	ctx, parent := provider.Tracer("registry-server-failure-test").Start(context.Background(), "request")
	if scoped {
		ctx = auth.ContextWithAuthContext(ctx, tenantAScopedAuthContext())
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// registryFailureMessage returns the fixed text of an error body in either
// shape: the plain {"detail": ...} body or the {"error": {"message": ...}}
// envelope.
func registryFailureMessage(body []byte) string {
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

func assertRegistryFailure(
	t *testing.T, message string, tc registryFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan,
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
		assertRegistryFenceVerdict(t, rec)
		return
	}
	if got := registryFailureMessage(rec.Body.Bytes()); got != message {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, message, body)
	}
	assertRegistryFailureSpan(t, message, tc.cancel, ended)
}

// assertRegistryFailureSpan checks the handler span: a client cancel leaves
// the status Unset with only the cancel event; a server fault sets Error with
// message as its description and records exactly one exception event.
func assertRegistryFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	span := registryFailureSpan(t, ended)
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

// assertRegistryFenceVerdict checks the reader-fence 503 from
// querycontract.WriteGraphReadError: backend_unavailable with Retry-After.
func assertRegistryFenceVerdict(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("reader fence 503 is missing Retry-After; body = %s", rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil ||
		envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
		t.Fatalf("reader fence body = %s (decode err %v), want a backend_unavailable envelope", rec.Body.String(), err)
	}
}

// registryFailureSpan returns the route's own handler span: the first
// recorded span that is not the test's request span.
func registryFailureSpan(t *testing.T, ended []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range ended {
		if span.Name() != "request" {
			return span
		}
	}
	t.Fatal("no handler span recorded")
	return nil
}

// failingRegistryGraph fails its failCall-th Run (1-based; 0 fails every
// call, a negative value fails none) with err. Every other Run answers from answers keyed by the exact
// Cypher statement, falling back to rows.
type failingRegistryGraph struct {
	err      error
	failCall int
	answers  map[string][]map[string]any
	rows     []map[string]any
	calls    int
}

func (g *failingRegistryGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	g.calls++
	if g.failCall == 0 || g.calls == g.failCall {
		return nil, g.err
	}
	if rows, ok := g.answers[cypher]; ok {
		return rows, nil
	}
	return g.rows, nil
}

func (*failingRegistryGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// failingRegistryCorrelations fails every correlation read with err.
type failingRegistryCorrelations struct{ err error }

func (s failingRegistryCorrelations) ListPackageRegistryCorrelations(
	context.Context, CorrelationFilter,
) (CorrelationPage, error) {
	return CorrelationPage{}, s.err
}
