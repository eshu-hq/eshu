// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// supplyFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const supplyFailureCanary = "password=hunter2 MATCH (n)"

const (
	stageFailedEvent   = "supply_chain_query.stage_failed"
	stageCanceledEvent = "supply_chain_query.stage_canceled"
)

// supplyFailureKind is how a backend read fails in one table case.
type supplyFailureKind int

const (
	// failureFault is a server fault whose text carries the canary.
	failureFault supplyFailureKind = iota
	// failureClientCancel is the caller canceling its own request.
	failureClientCancel
	// failureInnerCancel is a context.Canceled from an inner context while
	// the request is still live: a server fault, not a client cancel.
	failureInnerCancel
	// failureReaderFence is a stale guarded PostgreSQL reader.
	failureReaderFence
)

type supplyFailureCase struct {
	name string
	kind supplyFailureKind
	err  error
}

func supplyFailureCases() []supplyFailureCase {
	canceled := fmt.Errorf("backend: %s: %w", supplyFailureCanary, context.Canceled)
	return []supplyFailureCase{
		{name: "backend failure", kind: failureFault, err: errors.New("backend: " + supplyFailureCanary)},
		{name: "client cancel", kind: failureClientCancel, err: canceled},
		{name: "inner context cancel on a live request", kind: failureInnerCancel, err: canceled},
		{name: "reader stale", kind: failureReaderFence, err: fmt.Errorf("backend: %s: %w", supplyFailureCanary, db.ErrReaderStale)},
	}
}

// supplyFailureRoute is one converted failure site: the request that reaches
// it, the handler whose read for that step fails with err, and the stage log
// identity and fixed message the step answers with.
type supplyFailureRoute struct {
	name      string
	method    string // empty means GET
	target    string
	body      string
	authCtx   *auth.AuthContext
	spanName  string
	operation string
	// stage is the stage-log stage; empty means the step has no stage timer.
	stage   string
	message string
	// writePath marks a writer step with no reader fence to map: a stale
	// reader error there is an ordinary server fault.
	writePath bool
	build     func(err error) *Handler
}

// runSupplyFailureRoutes drives every route through every failure case. It
// swaps the package-level queryHandlerTracer, so callers must not be parallel.
func runSupplyFailureRoutes(t *testing.T, routes []supplyFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range supplyFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				serveSupplyFailure(t, route, tc)
			})
		}
	}
}

func serveSupplyFailure(t *testing.T, route supplyFailureRoute, tc supplyFailureCase) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("supply-chain-server-failure-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	ctx := context.Background()
	if route.authCtx != nil {
		ctx = auth.ContextWithAuthContext(ctx, *route.authCtx)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if tc.kind == failureClientCancel {
		cancel()
	}
	var logBuf bytes.Buffer
	handler := route.build(tc.err)
	handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))
	mux := http.NewServeMux()
	handler.Mount(mux)
	method := route.method
	if method == "" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, route.target, strings.NewReader(route.body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertSupplyFailure(t, route, tc, rec, decodeLogRecords(t, &logBuf), recorder.Ended())
}

func assertSupplyFailure(
	t *testing.T,
	route supplyFailureRoute,
	tc supplyFailureCase,
	rec *httptest.ResponseRecorder,
	records []map[string]any,
	ended []sdktrace.ReadOnlySpan,
) {
	t.Helper()
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "MATCH (n)") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	fence := tc.kind == failureReaderFence && !route.writePath
	wantStatus := http.StatusInternalServerError
	switch {
	case tc.kind == failureClientCancel:
		wantStatus = querycontract.StatusClientClosedRequest
	case fence:
		wantStatus = http.StatusServiceUnavailable
	}
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	failed := recordsWithEvent(records, stageFailedEvent)
	canceled := recordsWithEvent(records, stageCanceledEvent)
	if fence {
		assertSupplyFenceVerdict(t, rec)
		if len(failed) != 0 || len(canceled) != 0 {
			t.Fatalf("fence verdict logged stage_failed=%d stage_canceled=%d, want 0/0", len(failed), len(canceled))
		}
		return
	}
	if !detailEquals(rec.Body.Bytes(), route.message) {
		t.Fatalf("body = %s, want the fixed detail %q", body, route.message)
	}

	span := supplyHandlerSpan(t, ended, route.spanName)
	exceptions, hasCanceled := 0, false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			exceptions++
		}
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := span.Status()
	if tc.kind == failureClientCancel {
		if status.Code != codes.Unset || exceptions != 0 || !hasCanceled {
			t.Fatalf("cancel span status = %v, exceptions = %d, %s = %v; want Unset, 0, true",
				status.Code, exceptions, tracing.ClientCanceledEvent, hasCanceled)
		}
		if route.stage != "" {
			assertStageCanceled(t, route, failed, canceled)
		}
		return
	}
	if status.Code != codes.Error || status.Description != route.message || exceptions != 1 || hasCanceled {
		t.Fatalf("fault span status = %v (%q), exceptions = %d, canceled event = %v; want Error (%q), 1, false",
			status.Code, status.Description, exceptions, hasCanceled, route.message)
	}
	if route.stage == "" {
		return
	}
	wantCause := "unknown"
	if tc.kind == failureInnerCancel {
		wantCause = "canceled"
	}
	assertStageFailed(t, route, failed, canceled, wantCause)
}

func assertStageCanceled(t *testing.T, route supplyFailureRoute, failed, canceled []map[string]any) {
	t.Helper()
	if len(failed) != 0 {
		t.Fatalf("stage_failed records = %d on a client cancel, want 0: %#v", len(failed), failed)
	}
	if len(canceled) != 1 {
		t.Fatalf("stage_canceled records = %d, want 1", len(canceled))
	}
	record := canceled[0]
	if record["level"] != "INFO" || record["operation"] != route.operation || record["stage"] != route.stage {
		t.Fatalf("stage_canceled = %#v, want INFO operation=%q stage=%q", record, route.operation, route.stage)
	}
	if _, ok := record["repo_id"]; !ok {
		t.Fatalf("stage_canceled = %#v, want a repo_id attribute", record)
	}
	if _, ok := record["duration_seconds"].(float64); !ok {
		t.Fatalf("stage_canceled = %#v, want a numeric duration_seconds", record)
	}
	for _, key := range []string{"error", "error_site", "error_cause"} {
		if _, ok := record[key]; ok {
			t.Fatalf("stage_canceled carries %q, want no error attributes: %#v", key, record)
		}
	}
}

func assertStageFailed(t *testing.T, route supplyFailureRoute, failed, canceled []map[string]any, wantCause string) {
	t.Helper()
	if len(canceled) != 0 {
		t.Fatalf("stage_canceled records = %d on a server fault, want 0", len(canceled))
	}
	if len(failed) != 1 {
		t.Fatalf("stage_failed records = %d, want 1", len(failed))
	}
	record := failed[0]
	if record["level"] != "ERROR" || record["operation"] != route.operation || record["stage"] != route.stage {
		t.Fatalf("stage_failed = %#v, want ERROR operation=%q stage=%q", record, route.operation, route.stage)
	}
	if record["error_cause"] != wantCause {
		t.Fatalf("stage_failed error_cause = %v, want %q", record["error_cause"], wantCause)
	}
}

// assertSupplyFenceVerdict checks the reader-fence 503 from
// querycontract.WriteGraphReadError: backend_unavailable with Retry-After.
func assertSupplyFenceVerdict(t *testing.T, rec *httptest.ResponseRecorder) {
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

// detailEquals reports whether body is a plain error whose detail is want.
func detailEquals(body []byte, want string) bool {
	var plain struct {
		Detail string `json:"detail"`
	}
	return json.Unmarshal(body, &plain) == nil && plain.Detail == want
}

// assertFaultSpan checks the handler span of a server fault: Error status with
// the route's fixed message as its description and exactly one exception
// event, so no second marker (such as the removed failStage) records it again.
func assertFaultSpan(t *testing.T, span sdktrace.ReadOnlySpan, message string) {
	t.Helper()
	exceptions := 0
	for _, event := range span.Events() {
		if event.Name == "exception" {
			exceptions++
		}
	}
	status := span.Status()
	if status.Code != codes.Error || status.Description != message || exceptions != 1 {
		t.Fatalf("handler span status = %v (%q), exceptions = %d; want Error (%q), 1",
			status.Code, status.Description, exceptions, message)
	}
}

// supplyHandlerSpan returns the ended route handler span named spanName.
func supplyHandlerSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, spanName string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range ended {
		if span.Name() == spanName {
			return span
		}
	}
	t.Fatalf("no ended %s span", spanName)
	return nil
}
