// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
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
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// impactFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const impactFailureCanary = "password=hunter2 MATCH (n)"

// impactFailureCase is one way a backend read fails: a server fault whose
// text carries the canary, a client cancel, or a stale PostgreSQL reader.
type impactFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func impactFailureCases() []impactFailureCase {
	return []impactFailureCase{
		{name: "backend failure", err: errors.New("backend: " + impactFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", impactFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", impactFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// impactFailureRoute is one converted failure site: the request that reaches
// it, the handler whose read for that step fails with err, and the fixed
// message and status a server fault answers.
type impactFailureRoute struct {
	name    string
	path    string
	body    string
	message string
	// faultStatus is the status a server fault answers; zero means 500.
	faultStatus int
	handler     func(err error) *Handler
	// authCtx, when set, is the caller's auth; nil is an unscoped caller.
	authCtx *auth.AuthContext
}

// runImpactFailureRoutes drives every route through every failure case. It
// swaps the package-level queryHandlerTracer, so callers must not be parallel
// and the subtests run one at a time.
func runImpactFailureRoutes(t *testing.T, routes []impactFailureRoute) {
	t.Helper()
	for _, route := range routes {
		for _, tc := range impactFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				serveImpactFailure(t, route, tc)
			})
		}
	}
}

func serveImpactFailure(t *testing.T, route impactFailureRoute, tc impactFailureCase) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("impact-server-failure-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	ctx, parent := provider.Tracer("impact-server-failure-test").Start(context.Background(), "request")
	if route.authCtx != nil {
		ctx = auth.ContextWithAuthContext(ctx, *route.authCtx)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if tc.cancel {
		cancel()
	}
	mux := http.NewServeMux()
	route.handler(tc.err).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()

	assertImpactFailure(t, route, tc, rec, recorder.Ended())
}

// serveImpactRequest posts body to path on handler's routes with no span or
// tracer swap, for single-status checks that can run in parallel.
func serveImpactRequest(t *testing.T, handler *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

// containsDetail reports whether body is a plain error whose detail is want.
func containsDetail(body []byte, want string) bool {
	var plain struct {
		Detail string `json:"detail"`
	}
	return json.Unmarshal(body, &plain) == nil && plain.Detail == want
}

func assertImpactFailure(t *testing.T, route impactFailureRoute, tc impactFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "MATCH (n)") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	wantStatus := route.faultStatus
	if wantStatus == 0 {
		wantStatus = http.StatusInternalServerError
	}
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
		assertImpactFenceVerdict(t, rec)
		return
	}
	// A cancel can surface at an earlier step whose own read saw the canceled
	// context, so only a server fault pins this step's message.
	if !tc.cancel && !containsDetail(rec.Body.Bytes(), route.message) {
		t.Fatalf("body = %s, want the fixed detail %q", body, route.message)
	}

	span := impactFailureSpan(t, ended)
	hasException, hasCanceled := false, false
	for _, event := range span.Events() {
		hasException = hasException || event.Name == "exception"
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := span.Status()
	if tc.cancel {
		if status.Code != codes.Unset || hasException || !hasCanceled {
			t.Fatalf("cancel span status = %v, exception = %v, %s = %v; want Unset, false, true",
				status.Code, hasException, tracing.ClientCanceledEvent, hasCanceled)
		}
		return
	}
	if status.Code != codes.Error || status.Description != route.message || !hasException || hasCanceled {
		t.Fatalf("fault span status = %v (%q), exception = %v, canceled event = %v; want Error (%q), true, false",
			status.Code, status.Description, hasException, hasCanceled, route.message)
	}
}

// assertImpactFenceVerdict checks the reader-fence 503 from
// querycontract.WriteGraphReadError: backend_unavailable with Retry-After.
func assertImpactFenceVerdict(t *testing.T, rec *httptest.ResponseRecorder) {
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

// impactFailureSpan returns the innermost recorded span: the route's own
// handler span when it opens one, otherwise the test's request span.
func impactFailureSpan(t *testing.T, ended []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	var request sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Name() != "request" {
			return span
		}
		request = span
	}
	if request == nil {
		t.Fatal("no span recorded")
	}
	return request
}

// failingImpactGraph fails every graph read whose Cypher fail matches (all of
// them when fail is nil) and answers the rest from answer, or with no rows.
type failingImpactGraph struct {
	err    error
	fail   func(cypher string) bool
	answer func(cypher string, params map[string]any) []map[string]any
}

func (g failingImpactGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if g.fail == nil || g.fail(cypher) {
		return nil, g.err
	}
	if g.answer == nil {
		return nil, nil
	}
	return g.answer(cypher, params), nil
}

func (g failingImpactGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// cypherContains returns a fail predicate matching Cypher that contains
// fragment.
func cypherContains(fragment string) func(string) bool {
	return func(cypher string) bool { return strings.Contains(cypher, fragment) }
}

// failingImpactContent fails the content reads the impact routes issue: entity
// lookup by id, entity search by name, and repository entity listing. A nil
// err for one of them falls through to the embedded fake.
type failingImpactContent struct {
	content.FakePortContentStore
	getErr    error
	searchErr error
	listErr   error
	entity    *querycontract.EntityContent
	search    []querycontract.EntityContent
}

func (s failingImpactContent) GetEntityContent(ctx context.Context, id string) (*querycontract.EntityContent, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.entity != nil {
		return s.entity, nil
	}
	return s.FakePortContentStore.GetEntityContent(ctx, id)
}

func (s failingImpactContent) SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	if s.search != nil {
		return s.search, nil
	}
	return s.FakePortContentStore.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
}

func (s failingImpactContent) ListRepoEntities(ctx context.Context, repoID string, limit int) ([]querycontract.EntityContent, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.FakePortContentStore.ListRepoEntities(ctx, repoID, limit)
}

// stubImpactTraceContext answers the deployment-trace service context with
// workload, or fails it with err.
type stubImpactTraceContext struct {
	workload map[string]any
	err      error
}

func (s stubImpactTraceContext) FetchServiceTraceContext(
	context.Context, querycontract.GraphQuery, querycontract.ContentStore, *slog.Logger,
	*telemetry.Instruments, string, TraceEnrichmentConfig,
) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	workload := make(map[string]any, len(s.workload))
	for key, value := range s.workload {
		workload[key] = value
	}
	return workload, nil
}

func (stubImpactTraceContext) BuildServiceDeploymentOverview(map[string]any) map[string]any {
	return map[string]any{}
}

// stubImpactCodeSurface answers change-surface code evidence with an empty
// surface, or fails it with err.
type stubImpactCodeSurface struct{ err error }

func (s stubImpactCodeSurface) FetchCodeSurface(
	context.Context, *Handler, ChangeSurfaceInvestigationRequest,
	func(context.Context, ChangeSurfaceInvestigationRequest) ([]map[string]any, bool, error),
) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return map[string]any{}, nil
}
