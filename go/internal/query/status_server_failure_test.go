// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// statusFailureRoute is one converted status-route failure site plus the
// capability its reader-fence envelope must name. An empty capability means
// the route has no capability-matrix key (#8227), so the envelope must carry
// no capability field at all.
type statusFailureRoute struct {
	route      failure.Route
	capability string
}

// statusFailureRoutes lists every status read whose failure #7674 slice 9
// moved onto a fixed message. None of these routes opens its own handler span,
// so the harness reads the failure off the request span.
func statusFailureRoutes() []statusFailureRoute {
	statusRead := func(path, message string, build func(error) *StatusHandler) failure.Route {
		return failure.Route{
			Name: path, Path: path, Message: message,
			Handler: func(err error) failure.Mounter { return build(err) },
		}
	}
	readerFails := func(err error) *StatusHandler {
		return &StatusHandler{StatusReader: fakeStatusReader{err: err}, LiveActivity: &fakeLiveActivityReader{}}
	}
	return []statusFailureRoute{
		{route: statusRead("/api/v0/status/pipeline", pipelineStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/collectors", collectorStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/collectors", collectorStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/ingesters/repository", ingesterStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/ingesters/repository", ingesterStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/index", indexStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/index-status", indexStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/hosted-readiness", hostedReadinessStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/freshness-causality", freshnessCausalityStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/status/collector-readiness", collectorReadinessStatusReadFailedMessage, readerFails)},
		{route: statusRead("/api/v0/collector-readiness", collectorReadinessStatusReadFailedMessage, readerFails)},
		{
			route:      statusRead("/api/v0/status/semantic-extraction", semanticExtractionStatusReadFailedMessage, readerFails),
			capability: semanticExtractionStatusCapability,
		},
		{
			route:      statusRead("/api/v0/status/operations", operationsStatusReadFailedMessage, readerFails),
			capability: operationsStatusCapability,
		},
		{
			route: statusRead("/api/v0/status/operations?limit=5", operationsLiveActivityReadFailedMessage,
				func(err error) *StatusHandler {
					return &StatusHandler{StatusReader: fakeStatusReader{}, LiveActivity: &fakeLiveActivityReader{err: err}}
				}),
			capability: operationsStatusCapability,
		},
		{
			route:      statusRead("/api/v0/status/governance", governanceStatusReadFailedMessage, readerFails),
			capability: hostedGovernanceStatusCapability,
		},
		{
			route: statusRead("/api/v0/status/governance?audit=1", governanceAuditSummaryReadFailedMessage,
				func(err error) *StatusHandler {
					return &StatusHandler{StatusReader: fakeStatusReader{}, GovernanceAudit: fakeGovernanceAuditSummaryReader{err: err}}
				}),
			capability: hostedGovernanceStatusCapability,
		},
		{
			route:      statusRead("/api/v0/status/answer-narration", answerNarrationStatusReadFailedMessage, readerFails),
			capability: answerNarrationStatusCapability,
		},
	}
}

// TestStatusRoutesAnswerFixedServerFailures is the #7674 slice 9 regression
// for the status read routes. Each answered 500 with the reader's error text
// (fmt.Sprintf("load status: %v", err)), answered a stale reader with that
// 500 instead of the retryable 503, and answered a client cancel as a server
// fault. The status snapshot reads through the guarded PostgreSQL reader, so a
// stale reader must answer 503 with Retry-After.
func TestStatusRoutesAnswerFixedServerFailures(t *testing.T) {
	routes := statusFailureRoutes()
	suite := failure.Suite{}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// TestScopedIndexStatusAnswersFixedServerFailures covers the scoped caller of
// GET /api/v0/status/index and GET /api/v0/index-status, whose only read is
// the granted-repository count in the graph. It answered 500 with the graph
// error text, and a graph outage or deadline also answered that 500.
func TestScopedIndexStatusAnswersFixedServerFailures(t *testing.T) {
	scope := func(ctx context.Context) context.Context {
		return ContextWithAuthContext(ctx, AuthContext{
			Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w",
			AllowedRepositoryIDs: []string{indexStatusRepoA},
		})
	}
	var routes []failure.Route
	for _, path := range indexStatusScopedPaths {
		routes = append(routes, failure.Route{
			Name: "scoped " + path, Path: path, Message: scopedRepositoryCountFailedMessage, Scope: scope,
			Handler: func(err error) failure.Mounter {
				return &StatusHandler{Neo4j: failingRunSingleGraph{err: err}}
			},
		})
	}
	suite := failure.Suite{Routes: routes}
	failure.Run(t, suite)

	for _, route := range routes {
		for _, tc := range []struct {
			name       string
			err        error
			wantStatus int
		}{
			{name: "graph unavailable", err: fmt.Errorf("%s: %w", failure.Canary, ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable},
			{name: "graph deadline", err: fmt.Errorf("%s: %w", failure.Canary, ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout},
		} {
			rec, _ := failure.Serve(suite, route, route.Handler(tc.err), false)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s / %s: status = %d, want %d; body = %s", route.Name, tc.name, rec.Code, tc.wantStatus, rec.Body.String())
			}
		}
		// A count that answers no row is a server fault with no error in
		// scope: it answers the same fixed message, never a 0.
		rec, _ := failure.Serve(suite, route, &StatusHandler{Neo4j: failingRunSingleGraph{}}, false)
		if rec.Code != http.StatusInternalServerError || failure.Message(rec.Body.Bytes()) != scopedRepositoryCountFailedMessage {
			t.Errorf("%s / no row: status = %d, body = %s; want 500 %q", route.Name, rec.Code, rec.Body.String(), scopedRepositoryCountFailedMessage)
		}
		assertFenceCapability(t, suite, route, "")
	}
}

// assertFenceCapability serves route with a stale-reader error and checks the
// envelope's capability field: equal to capability when it is set, absent
// when it is empty (a route with no capability-matrix key, #8227).
func assertFenceCapability(t *testing.T, suite failure.Suite, route failure.Route, capability string) {
	t.Helper()
	rec, _ := failure.Serve(suite, route, route.Handler(fmt.Errorf("%s: %w", failure.Canary, db.ErrReaderStale)), false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("%s / fence capability: status = %d, want 503; body = %s", route.Name, rec.Code, rec.Body.String())
		return
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Errorf("%s / fence capability: decode body: %v; body = %s", route.Name, err, rec.Body.String())
		return
	}
	got, present := body.Error["capability"]
	switch {
	case capability == "" && present:
		t.Errorf("%s / fence capability: envelope carries capability %v, want no field (#8227)", route.Name, got)
	case capability != "" && got != capability:
		t.Errorf("%s / fence capability: envelope capability = %v, want %q", route.Name, got, capability)
	}
}

// failingRunSingleGraph fails every RunSingle with err, or answers no row
// when err is nil.
type failingRunSingleGraph struct{ err error }

func (g failingRunSingleGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, g.err
}

func (g failingRunSingleGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, g.err
}
