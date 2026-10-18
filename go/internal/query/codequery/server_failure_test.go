// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// codeFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674). The
// sentinel test below wraps it around the unavailable sentinels; the route
// tests drive the helper's own canary through failure.Run.
const codeFailureCanary = "password=hunter2 MATCH (n)"

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

// assertNoCodeFailureLeak fails when any part of the canary reached the body.
// The sentinel test below still uses it; the converted route tests rely on
// the helper's four-token canary scan instead.
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
		Detail string          `json:"detail"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	var envelope querycontract.ErrorEnvelope
	if body.Detail == "" && json.Unmarshal(body.Error, &envelope) == nil {
		return envelope.Message
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
// It swaps queryHandlerTracer through the suite, so it must not be parallel.
func TestCodeGraphReadFailuresAnswerFixedText(t *testing.T) {
	graphOnly := func(profile QueryProfile) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			return &CodeHandler{Neo4j: failingCodeGraph(err), Profile: profile}
		}
	}
	route := func(name, path, body, message string, handler func(error) failure.Mounter) failure.Route {
		return failure.Route{
			Name: name, Method: http.MethodPost, Path: path, Body: body, Message: message,
			Handler: handler,
		}
	}
	// Only some graph routes open a handler span before their read fails;
	// the rest record the failure on the request span, as the old harness
	// did by falling back to it. The helper checks the request span when
	// the suite names no HandlerTracer, so each group runs in its own
	// suite and keeps the span the production code actually marks.
	failure.Run(t, failure.Suite{
		HandlerTracer: &queryHandlerTracer,
		Routes: []failure.Route{
			route("call graph metrics", "/api/v0/code/call-graph/metrics", `{"repo_id":"repo-1"}`, callGraphMetricsFailedMessage, graphOnly("")),
			route("import dependencies", "/api/v0/code/imports/investigate", `{"repo_id":"repo-1"}`, importDependencyFailedMessage, graphOnly("")),
			route("dead code cross repo", "/api/v0/code/dead-code/cross-repo", `{"repo_id":"repo-1"}`, "cross-repo dead-code candidate scan failed", graphOnly(ProfileLocalAuthoritative)),
			route("dead code investigation", "/api/v0/code/dead-code/investigate", `{}`, "dead-code investigation scan failed", graphOnly(ProfileLocalAuthoritative)),
		},
	})
	failure.Run(t, failure.Suite{
		Routes: []failure.Route{
			route("search graph", "/api/v0/code/search", `{"query":"foo","repo_id":"repo-1"}`, codeSearchGraphFailedMessage, graphOnly("")),
			route("symbol search", "/api/v0/code/symbols/search", `{"symbol":"Foo","repo_id":"repo-1"}`, symbolSearchFailedMessage, graphOnly("")),
			route("relationships", "/api/v0/code/relationships", `{"entity_id":"e1"}`, relationshipGraphFailedMessage, graphOnly("")),
			route("relationship story", "/api/v0/code/relationships/story", `{"entity_id":"entity-1"}`, relationshipStoryFailedMessage, graphOnly("")),
			route("complexity ranking", "/api/v0/code/complexity", `{}`, complexityRankingFailedMessage, graphOnly("")),
			route("complexity lookup", "/api/v0/code/complexity", `{"entity_id":"e1"}`, complexityLookupFailedMessage, graphOnly("")),
			route("call chain", "/api/v0/code/call-chain", `{"start_entity_id":"s1","end_entity_id":"e1"}`, callChainGraphFailedMessage, graphOnly("")),
			route("route to caller", "/api/v0/code/routes/callers", `{"path":"/foo","repo_id":"repo-1"}`, routeToCallerRouteFailedMessage, graphOnly("")),
			route("quality inspect", "/api/v0/code/quality/inspect", `{"check":"complexity"}`, codeQualityFailedMessage, graphOnly(ProfileLocalAuthoritative)),
			route("compare paths", "/api/v0/code/call-chain/compare", `{"repo_id":"repo-1","start_entity_id":"a","end_entity_id":"e"}`, comparePathsGraphFailedMessage, graphOnly(ProfileLocalAuthoritative)),
			route("cypher", "/api/v0/code/cypher", `{"cypher_query":"RETURN 1"}`, readOnlyCypherFailedMessage, graphOnly("")),
			route("bundles", "/api/v0/code/bundles", `{"query":"x"}`, bundleSearchFailedMessage, graphOnly("")),
			route("dead code", "/api/v0/code/dead-code", `{}`, "dead-code candidate scan failed", graphOnly(ProfileLocalAuthoritative)),
		},
	})
}
