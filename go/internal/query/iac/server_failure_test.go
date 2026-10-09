// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

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

// iacFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const iacFailureCanary = "password=hunter2 MATCH (n)"

// iacFailureCase is one way a backend read fails: a server fault whose text
// carries the canary, a client cancel, or a stale PostgreSQL reader.
type iacFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func iacFailureCases() []iacFailureCase {
	return []iacFailureCase{
		{name: "backend failure", err: errors.New("backend: " + iacFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", iacFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", iacFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// iacFailureRoute is one converted failure site: the request that reaches it,
// the handler whose read for that step fails with err, and the fixed message
// a server fault answers.
type iacFailureRoute struct {
	name    string
	method  string
	path    string
	body    string
	message string
	handler func(err error) *Handler
}

// runIACFailureRoutes drives every route through every failure case. It swaps
// the package-level iacHandlerTracer, so callers must not be parallel and the
// subtests run one at a time.
func runIACFailureRoutes(t *testing.T, routes []iacFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range iacFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				rec, ended := serveIACTraced(t, route.handler(tc.err), route.method, route.path, route.body, tc.cancel)
				assertIACFailure(t, route.message, tc, rec, ended)
			})
		}
	}
}

// serveIACTraced serves one request under a recording tracer and returns the
// response and every ended span. cancel cancels the request context first.
func serveIACTraced(
	t *testing.T, handler *Handler, method, path, body string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := iacHandlerTracer
	iacHandlerTracer = provider.Tracer("iac-server-failure-test")
	t.Cleanup(func() { iacHandlerTracer = previous })

	ctx, parent := provider.Tracer("iac-server-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// iacFailureMessage returns the fixed text of an error body in either shape:
// the plain {"detail": ...} body or the {"error": {"message": ...}} envelope.
func iacFailureMessage(body []byte) string {
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

func assertIACFailure(t *testing.T, message string, tc iacFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan) {
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
		assertIACFenceVerdict(t, rec)
		return
	}
	if got := iacFailureMessage(rec.Body.Bytes()); got != message {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, message, body)
	}
	assertIACFailureSpan(t, message, tc.cancel, ended)
}

// assertIACFailureSpan checks the handler span: a client cancel leaves the
// status Unset with only the cancel event; a server fault sets Error with
// message as its description and records exactly one exception event.
func assertIACFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	span := iacFailureSpan(t, ended)
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

// assertIACFenceVerdict checks the reader-fence 503 from
// querycontract.WriteGraphReadError: backend_unavailable with Retry-After.
func assertIACFenceVerdict(t *testing.T, rec *httptest.ResponseRecorder) {
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

// iacFailureSpan returns the route's own handler span: the first recorded
// span that is not the test's request span.
func iacFailureSpan(t *testing.T, ended []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range ended {
		if span.Name() != "request" {
			return span
		}
	}
	t.Fatal("no handler span recorded")
	return nil
}

// failingManagementStore fails the management count, list, or selector read
// with the configured error and answers the rest from rows.
type failingManagementStore struct {
	countErr    error
	listErr     error
	selectorErr error
	rows        []ManagementFindingRow
}

func (s failingManagementStore) CountUnmanagedCloudResources(context.Context, ManagementFilter) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return len(s.rows), nil
}

func (s failingManagementStore) ListUnmanagedCloudResources(context.Context, ManagementFilter) ([]ManagementFindingRow, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.rows, nil
}

func (s failingManagementStore) ListReplatformingSelectors(context.Context, int, []string) (ReplatformingSelectorPage, error) {
	return ReplatformingSelectorPage{}, s.selectorErr
}

// failingInventoryStore fails the inventory search or facet summary with the
// configured error and answers the search from candidates otherwise.
type failingInventoryStore struct {
	searchErr  error
	summaryErr error
	candidates []InventoryCandidate
}

func (s failingInventoryStore) SearchActive(context.Context, InventorySearch, querycontract.RepositoryAccessFilter) ([]InventoryCandidate, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.candidates, nil
}

func (s failingInventoryStore) Summary(context.Context, querycontract.RepositoryAccessFilter, int) (InventorySummary, error) {
	return InventorySummary{}, s.summaryErr
}
