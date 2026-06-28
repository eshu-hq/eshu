// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRouteToCallerReturnsExactHandlerAndBoundedCallers(t *testing.T) {
	t.Parallel()

	var sawRouteQuery bool
	var sawTraversalQuery bool
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if isRouteToCallerSelectorQuery(cypher) {
					sawRouteQuery = true
					if !strings.Contains(cypher, "handler)-[route:HANDLES_ROUTE]->(endpoint:Endpoint)") {
						t.Fatalf("route query did not anchor on HANDLES_ROUTE: %s", cypher)
					}
					if got, want := params["repo_id"], "repo-payments"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					if got, want := params["path"], "/payments/{id}"; got != want {
						t.Fatalf("params[path] = %#v, want %#v", got, want)
					}
					if got, want := params["method"], "GET"; got != want {
						t.Fatalf("params[method] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"endpoint_id":        "endpoint-1",
						"path":               "/payments/{id}",
						"repo_id":            "repo-payments",
						"http_method":        "GET",
						"framework":          "fastapi",
						"handler_id":         "function-handler",
						"handler_name":       "getPayment",
						"handler_file_path":  "app/routes.py",
						"handler_language":   "python",
						"handler_start_line": int64(12),
						"handler_end_line":   int64(20),
					}}, nil
				}
				if strings.Contains(cypher, "CALLS") {
					sawTraversalQuery = true
					if got, want := params["handler_id"], "function-handler"; got != want {
						t.Fatalf("params[handler_id] = %#v, want %#v", got, want)
					}
					if got, want := params["limit"], 3; got != want {
						t.Fatalf("params[limit] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{"direction": "incoming", "depth": int64(1), "entity_id": "caller-1", "name": "authz", "file_path": "app/auth.py", "repo_id": "repo-payments"},
						{"direction": "incoming", "depth": int64(2), "entity_id": "caller-2", "name": "audit", "file_path": "app/audit.py", "repo_id": "repo-payments"},
						{"direction": "incoming", "depth": int64(2), "entity_id": "caller-3", "name": "metrics", "file_path": "app/metrics.py", "repo_id": "repo-payments"},
					}, nil
				}
				if isRouteToCallerImpactQuery(cypher) {
					if strings.Contains(cypher, "WHERE endpointWorkload IS NULL") ||
						strings.Contains(cypher, "WHERE runtimeWorkload IS NULL") ||
						strings.Contains(cypher, "WHERE repo IS NULL") {
						t.Fatalf("unscoped impact query filtered away optional matches: %s", cypher)
					}
					if !strings.Contains(cypher, "(handler)-[:RUNS_IN]->(runtimeWorkload:Workload)") {
						t.Fatalf("impact query did not use RUNS_IN Workload bridge: %s", cypher)
					}
					if !strings.Contains(cypher, "(repo:Repository)-[:EXPOSES_ENDPOINT]->(endpoint)") {
						t.Fatalf("impact query did not use Repository endpoint ownership: %s", cypher)
					}
					if strings.Contains(cypher, ":Service") ||
						strings.Contains(cypher, ":Deployable") ||
						strings.Contains(cypher, ":Deployment") {
						t.Fatalf("impact query used non-materialized platform labels: %s", cypher)
					}
					if got, want := params["endpoint_id"], "endpoint-1"; got != want {
						t.Fatalf("params[endpoint_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"endpoint_workloads": []any{map[string]any{"id": "workload-payments", "name": "payments", "repo_id": "repo-payments"}},
						"runtime_workloads":  []any{map[string]any{"id": "workload-payments", "name": "payments", "repo_id": "repo-payments"}},
						"repositories":       []any{map[string]any{"id": "repo-payments", "name": "payments-api"}},
					}}, nil
				}
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-payments","method":"get","path":"/payments/{id}","max_depth":2,"limit":2}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !sawRouteQuery || !sawTraversalQuery {
		t.Fatalf("sawRouteQuery=%v sawTraversalQuery=%v, want both true", sawRouteQuery, sawTraversalQuery)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["status"], "partial"; got != want {
		t.Fatalf("resp[status] = %#v, want %#v", got, want)
	}
	handlerResp, ok := resp["handler"].(map[string]any)
	if !ok {
		t.Fatalf("resp[handler] type = %T, want map[string]any", resp["handler"])
	}
	if got, want := handlerResp["entity_id"], "function-handler"; got != want {
		t.Fatalf("handler.entity_id = %#v, want %#v", got, want)
	}
	callers, ok := resp["callers"].([]any)
	if !ok {
		t.Fatalf("resp[callers] type = %T, want []any", resp["callers"])
	}
	if got, want := len(callers), 2; got != want {
		t.Fatalf("len(callers) = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("resp[truncated] = %#v, want %#v", got, want)
	}
	impact, ok := resp["impact"].(map[string]any)
	if !ok {
		t.Fatalf("resp[impact] type = %T, want map[string]any", resp["impact"])
	}
	workloads, ok := impact["workloads"].([]any)
	if !ok {
		t.Fatalf("impact.workloads type = %T, want []any", impact["workloads"])
	}
	if got, want := len(workloads), 1; got != want {
		t.Fatalf("len(impact.workloads) = %d, want %d", got, want)
	}
	repositories, ok := impact["repositories"].([]any)
	if !ok {
		t.Fatalf("impact.repositories type = %T, want []any", impact["repositories"])
	}
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("len(impact.repositories) = %d, want %d", got, want)
	}
}

func TestHandleRouteToCallerReportsUnsupportedWithoutHandlesRoute(t *testing.T) {
	t.Parallel()

	var traversalQueries int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if isRouteToCallerSelectorQuery(cypher) {
					return []map[string]any{{
						"endpoint_id": "endpoint-1",
						"path":        "/dynamic",
						"repo_id":     "repo-payments",
					}}, nil
				}
				if strings.Contains(cypher, "CALLS") {
					traversalQueries++
				}
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-payments","method":"GET","path":"/dynamic"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if traversalQueries != 0 {
		t.Fatalf("traversalQueries = %d, want 0 when HANDLES_ROUTE is absent", traversalQueries)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["status"], "unsupported"; got != want {
		t.Fatalf("resp[status] = %#v, want %#v", got, want)
	}
	if resp["handler"] != nil {
		t.Fatalf("resp[handler] = %#v, want nil", resp["handler"])
	}
}

func TestHandleRouteToCallerAmbiguousRouteIsConflict(t *testing.T) {
	t.Parallel()

	var traversalQueries int
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if isRouteToCallerSelectorQuery(cypher) {
					return []map[string]any{
						{
							"endpoint_id":  "endpoint-1",
							"path":         "/payments/{id}",
							"repo_id":      "repo-payments",
							"http_method":  "GET",
							"handler_id":   "handler-a",
							"handler_name": "getPaymentA",
						},
						{
							"endpoint_id":  "endpoint-1",
							"path":         "/payments/{id}",
							"repo_id":      "repo-payments",
							"http_method":  "GET",
							"handler_id":   "handler-b",
							"handler_name": "getPaymentB",
						},
					}, nil
				}
				if strings.Contains(cypher, "CALLS") {
					traversalQueries++
				}
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-payments","method":"GET","path":"/payments/{id}"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if traversalQueries != 0 {
		t.Fatalf("traversalQueries = %d, want 0 when route selector is ambiguous", traversalQueries)
	}
}

func TestHandleRouteToCallerServiceScopeUsesWorkloadEndpointEdges(t *testing.T) {
	t.Parallel()

	var sawWorkloadEndpointScope bool
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case isRouteToCallerSelectorQuery(cypher):
					sawWorkloadEndpointScope = true
					if !strings.Contains(cypher, "(workload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)") {
						t.Fatalf("service scope did not use materialized Workload endpoint edge: %s", cypher)
					}
					if strings.Contains(cypher, ":Service") {
						t.Fatalf("service scope used non-materialized Service endpoint edge: %s", cypher)
					}
					if got, want := params["service_id"], "workload-payments"; got != want {
						t.Fatalf("params[service_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"endpoint_id":  "endpoint-1",
						"path":         "/payments/{id}",
						"repo_id":      "repo-payments",
						"http_method":  "GET",
						"handler_id":   "function-handler",
						"handler_name": "getPayment",
					}}, nil
				case strings.Contains(cypher, "CALLS"):
					return nil, nil
				case isRouteToCallerImpactQuery(cypher):
					return []map[string]any{{
						"endpoint_workloads": []any{map[string]any{"id": "workload-payments", "name": "payments", "repo_id": "repo-payments"}},
						"runtime_workloads":  []any{},
						"repositories":       []any{map[string]any{"id": "repo-payments", "name": "payments-api"}},
					}}, nil
				default:
					t.Fatalf("unexpected query: %s", cypher)
					return nil, nil
				}
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"service_id":"workload-payments","method":"GET","path":"/payments/{id}"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !sawWorkloadEndpointScope {
		t.Fatal("route selector was not queried with workload endpoint scope")
	}
}

func TestHandleRouteToCallerScopedTraversalFiltersEveryPathNode(t *testing.T) {
	t.Parallel()

	var sawScopedPathPredicate bool
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case isRouteToCallerSelectorQuery(cypher):
					return []map[string]any{{
						"endpoint_id":  "endpoint-1",
						"path":         "/payments/{id}",
						"repo_id":      "repo-payments",
						"http_method":  "GET",
						"handler_id":   "function-handler",
						"handler_name": "getPayment",
					}}, nil
				case strings.Contains(cypher, "CALLS"):
					if !strings.Contains(cypher, "all(pathNode IN nodes(path) WHERE") {
						t.Fatalf("scoped traversal did not filter every path node: %s", cypher)
					}
					sawScopedPathPredicate = true
					return nil, nil
				case isRouteToCallerImpactQuery(cypher):
					return []map[string]any{{
						"endpoint_workloads": []any{},
						"runtime_workloads":  []any{},
						"repositories":       []any{},
					}}, nil
				default:
					t.Fatalf("unexpected query: %s", cypher)
					return nil, nil
				}
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-payments","method":"GET","path":"/payments/{id}"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-payments"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !sawScopedPathPredicate {
		t.Fatal("CALLS traversal query was not executed")
	}
}

func TestHandleRouteToCallerScopedRepoOutsideGrantIsNotFound(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				t.Fatal("graph should not be queried for a repo outside scoped grants")
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-other","method":"GET","path":"/payments/{id}"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-payments"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleRouteToCallerMissingRouteIsNotFound(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !isRouteToCallerSelectorQuery(cypher) {
					t.Fatalf("unexpected query: %s", cypher)
				}
				return nil, nil
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/routes/callers",
		bytes.NewBufferString(`{"repo_id":"repo-payments","method":"GET","path":"/missing"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func isRouteToCallerSelectorQuery(cypher string) bool {
	return strings.Contains(cypher, "route.http_method as http_method")
}

func isRouteToCallerImpactQuery(cypher string) bool {
	return strings.Contains(cypher, "endpoint_workloads")
}
