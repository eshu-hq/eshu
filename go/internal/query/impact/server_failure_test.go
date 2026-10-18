// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// impactFailureSuite wraps routes in a suite on the package handler tracer.
func impactFailureSuite(routes ...failure.Route) failure.Suite {
	return failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: routes}
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
	// ownSpan marks a step whose handler opens its own span with the package
	// tracer: its span checks read that span. Without it the failure is
	// answered on the request span, which is checked instead.
	ownSpan bool
	// cancelMessage is the message a client cancel answers when an earlier
	// step surfaces it first; empty means the route's own message.
	cancelMessage string
	handler       func(err error) *Handler
	// authCtx, when set, is the caller's auth; nil is an unscoped caller.
	authCtx *auth.AuthContext
}

// failureRoute converts the site to the shared harness route. Every impact
// failure route is a POST whose fixed message answers as a plain detail body.
func (route impactFailureRoute) failureRoute() failure.Route {
	converted := failure.Route{
		Name: route.name, Method: http.MethodPost, Path: route.path, Body: route.body,
		Message: route.message, PlainDetail: true,
		FaultStatus: route.faultStatus, CancelMessage: route.cancelMessage,
		Handler: func(err error) failure.Mounter { return route.handler(err) },
	}
	if route.authCtx != nil {
		authCtx := *route.authCtx
		converted.Scope = func(ctx context.Context) context.Context {
			return auth.ContextWithAuthContext(ctx, authCtx)
		}
	}
	return converted
}

// runImpactFailureRoutes drives every route through every failure case. A
// route whose handler opens its own span runs on the package tracer suite;
// the rest answer on the request span and run without it. The tracer suite
// swaps the package-level queryHandlerTracer, so callers must not be parallel
// and the subtests run one at a time.
func runImpactFailureRoutes(t *testing.T, routes []impactFailureRoute) {
	t.Helper()
	var spanned, spanless []failure.Route
	for _, route := range routes {
		converted := route.failureRoute()
		if route.ownSpan {
			spanned = append(spanned, converted)
		} else {
			spanless = append(spanless, converted)
		}
	}
	if len(spanned) != 0 {
		failure.Run(t, impactFailureSuite(spanned...))
	}
	if len(spanless) != 0 {
		failure.Run(t, failure.Suite{Routes: spanless})
	}
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
