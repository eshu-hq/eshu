// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

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

// codeFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const codeFailureCanary = "password=hunter2 MATCH (n)"

// codeFailureCase is one way a code read fails: a server fault whose text
// carries the canary, a client cancel, or a stale PostgreSQL reader.
type codeFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func codeFailureCases() []codeFailureCase {
	return []codeFailureCase{
		{name: "backend failure", err: errors.New("backend: " + codeFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", codeFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", codeFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// codeFailureRoute is one code route step and the handler that fails it with
// the given error.
type codeFailureRoute struct {
	name    string
	path    string
	body    string
	message string
	handler func(err error) *CodeHandler
}

// runCodeFailureRoutes drives every route through every failure case. It
// swaps the package-level queryHandlerTracer, so callers must not be parallel.
func runCodeFailureRoutes(t *testing.T, routes []codeFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range codeFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				rec, ended := serveCodeTraced(t, route.handler(tc.err), route.path, route.body, tc.cancel)
				assertCodeFailure(t, route.message, tc, rec, ended)
			})
		}
	}
}

// failingCodeGraph fails every graph read with err.
func failingCodeGraph(err error) fakeGraphReader {
	fail := func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, err }
	return fakeGraphReader{
		run:         fail,
		runIncoming: fail,
		runSingle:   func(context.Context, string, map[string]any) (map[string]any, error) { return nil, err },
	}
}

// serveCodeTraced serves one POST under a recording tracer and returns the
// response and every ended span. cancel cancels the request context first.
func serveCodeTraced(
	t *testing.T, handler *CodeHandler, path, body string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("codequery-server-failure-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	ctx, parent := provider.Tracer("codequery-server-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// assertCodeFailure checks the status, the fixed body, and the span for one
// failure case.
func assertCodeFailure(
	t *testing.T, message string, tc codeFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan,
) {
	t.Helper()
	assertNoCodeFailureLeak(t, rec)
	wantStatus := http.StatusInternalServerError
	switch {
	case tc.cancel:
		wantStatus = querycontract.StatusClientClosedRequest
	case tc.stale:
		wantStatus = http.StatusServiceUnavailable
	}
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	if tc.stale {
		var envelope querycontract.ResponseEnvelope
		if rec.Header().Get("Retry-After") == "" || json.Unmarshal(rec.Body.Bytes(), &envelope) != nil ||
			envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
			t.Fatalf("reader fence answer = %s (Retry-After %q), want a backend_unavailable envelope with Retry-After",
				rec.Body.String(), rec.Header().Get("Retry-After"))
		}
		return
	}
	if got := codeFailureBodyMessage(t, rec); got != message {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, message, rec.Body.String())
	}
	assertCodeFailureSpan(t, message, tc.cancel, ended)
}

// assertNoCodeFailureLeak fails when any part of the canary reached the body.
func assertNoCodeFailureLeak(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "MATCH (n)") || strings.Contains(body, "backend:") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
}

// codeFailureBodyMessage reads the client-facing message from a plain
// {"detail"} body or from a {data, error} envelope.
func codeFailureBodyMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Detail string                       `json:"detail"`
		Error  *querycontract.ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	if body.Error != nil {
		return body.Error.Message
	}
	return body.Detail
}

// assertCodeFailureSpan checks the span the failure marked: the handler span
// when the route starts one, otherwise the request span. A client cancel
// leaves the status Unset with only the cancel event; a server fault sets
// Error with message as its description and records exactly one exception.
func assertCodeFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	var span sdktrace.ReadOnlySpan
	for _, candidate := range ended {
		if candidate.Name() != "request" || span == nil {
			span = candidate
		}
	}
	if span == nil {
		t.Fatal("no span recorded")
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
			t.Fatalf("span %q cancel status = %v, exceptions = %d, %s = %v; want Unset, 0, true",
				span.Name(), status.Code, exceptions, tracing.ClientCanceledEvent, hasCanceled)
		}
		return
	}
	if status.Code != codes.Error || status.Description != message || exceptions != 1 || hasCanceled {
		t.Fatalf("span %q fault status = %v (%q), exceptions = %d, canceled event = %v; want Error (%q), 1, false",
			span.Name(), status.Code, status.Description, exceptions, hasCanceled, message)
	}
}

// TestCodeGraphReadFailuresAnswerFixedText drives the graph-backed code route
// steps through a backend fault, a client cancel, and a stale reader (#7674).
// It swaps queryHandlerTracer, so it must not be parallel.
func TestCodeGraphReadFailuresAnswerFixedText(t *testing.T) {
	graphOnly := func(profile QueryProfile) func(error) *CodeHandler {
		return func(err error) *CodeHandler { return &CodeHandler{Neo4j: failingCodeGraph(err), Profile: profile} }
	}
	runCodeFailureRoutes(t, []codeFailureRoute{
		{"search graph", "/api/v0/code/search", `{"query":"foo","repo_id":"repo-1"}`, codeSearchGraphFailedMessage, graphOnly("")},
		{"symbol search", "/api/v0/code/symbols/search", `{"symbol":"Foo","repo_id":"repo-1"}`, symbolSearchFailedMessage, graphOnly("")},
		{"relationships", "/api/v0/code/relationships", `{"entity_id":"e1"}`, relationshipGraphFailedMessage, graphOnly("")},
		{"relationship story", "/api/v0/code/relationships/story", `{"entity_id":"entity-1"}`, relationshipStoryFailedMessage, graphOnly("")},
		{"complexity ranking", "/api/v0/code/complexity", `{}`, complexityRankingFailedMessage, graphOnly("")},
		{"complexity lookup", "/api/v0/code/complexity", `{"entity_id":"e1"}`, complexityLookupFailedMessage, graphOnly("")},
		{"call chain", "/api/v0/code/call-chain", `{"start_entity_id":"s1","end_entity_id":"e1"}`, callChainGraphFailedMessage, graphOnly("")},
		{"call graph metrics", "/api/v0/code/call-graph/metrics", `{"repo_id":"repo-1"}`, callGraphMetricsFailedMessage, graphOnly("")},
		{"import dependencies", "/api/v0/code/imports/investigate", `{"repo_id":"repo-1"}`, importDependencyFailedMessage, graphOnly("")},
		{"route to caller", "/api/v0/code/routes/callers", `{"path":"/foo","repo_id":"repo-1"}`, routeToCallerRouteFailedMessage, graphOnly("")},
		{"quality inspect", "/api/v0/code/quality/inspect", `{"check":"complexity"}`, codeQualityFailedMessage, graphOnly(ProfileLocalAuthoritative)},
		{"compare paths", "/api/v0/code/call-chain/compare", `{"repo_id":"repo-1","start_entity_id":"a","end_entity_id":"e"}`, comparePathsGraphFailedMessage, graphOnly(ProfileLocalAuthoritative)},
		{"cypher", "/api/v0/code/cypher", `{"cypher_query":"RETURN 1"}`, readOnlyCypherFailedMessage, graphOnly("")},
		{"bundles", "/api/v0/code/bundles", `{"query":"x"}`, bundleSearchFailedMessage, graphOnly("")},
		{"dead code", "/api/v0/code/dead-code", `{}`, "dead-code candidate scan failed", graphOnly(ProfileLocalAuthoritative)},
		{"dead code cross repo", "/api/v0/code/dead-code/cross-repo", `{"repo_id":"repo-1"}`, "cross-repo dead-code candidate scan failed", graphOnly(ProfileLocalAuthoritative)},
		{"dead code investigation", "/api/v0/code/dead-code/investigate", `{}`, "dead-code investigation scan failed", graphOnly(ProfileLocalAuthoritative)},
	})
}
