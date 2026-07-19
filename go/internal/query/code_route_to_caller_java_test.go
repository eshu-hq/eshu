// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleRouteToCallerResolvesJavaSpringHandler proves the trace_route_callers
// query surface (handleRouteToCaller, the go_symbol/mcp_tool the java row's
// language-feature-parity ledger entry claims per #5333) resolves an
// exact-anchored Java Spring HANDLES_ROUTE edge end to end: the endpoint
// lookup, handler resolution, and bounded caller/callee traversal all run
// through the real production handler and surface Java-language route/handler
// evidence, not just the already-covered Python/fastapi shape.
func TestHandleRouteToCallerResolvesJavaSpringHandler(t *testing.T) {
	t.Parallel()

	var sawRouteQuery, sawHandlerQuery bool
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if isRouteToCallerLabelResolveQuery(cypher) {
					return map[string]any{"label": "Function"}, nil
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case isRouteToCallerEndpointQuery(cypher):
					sawRouteQuery = true
					if got, want := params["repo_id"], "repo-billing"; got != want {
						t.Fatalf("endpoint params[repo_id] = %#v, want %#v", got, want)
					}
					if got, want := params["path"], "/api/reports/{id}"; got != want {
						t.Fatalf("endpoint params[path] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"endpoint_id": "endpoint-report", "path": "/api/reports/{id}",
						"repo_id": "repo-billing", "endpoint_framework": "spring",
					}}, nil
				case isRouteToCallerHandlerQuery(cypher):
					sawHandlerQuery = true
					return []map[string]any{{
						"endpoint_id": "endpoint-report", "http_method": "GET", "route_framework": "spring",
						"handler_id": "content-entity:report-get", "handler_name": "get",
						"handler_file_path":  "src/main/java/example/ReportController.java",
						"handler_language":   "java",
						"handler_start_line": int64(14), "handler_end_line": int64(17),
					}}, nil
				case isRouteToCallerDirectionQuery(cypher):
					return nil, nil
				case isRouteToCallerImpactQuery(cypher):
					return nil, nil
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
		bytes.NewBufferString(`{"repo_id":"repo-billing","method":"get","path":"/api/reports/{id}","max_depth":2,"limit":5}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !sawRouteQuery || !sawHandlerQuery {
		t.Fatalf("sawRouteQuery=%v sawHandlerQuery=%v, want both true", sawRouteQuery, sawHandlerQuery)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["status"], "complete"; got != want {
		t.Fatalf("resp[status] = %#v, want %#v", got, want)
	}
	route, ok := resp["route"].(map[string]any)
	if !ok {
		t.Fatalf("resp[route] type = %T, want map[string]any", resp["route"])
	}
	if got, want := route["framework"], "spring"; got != want {
		t.Fatalf("route.framework = %#v, want %#v", got, want)
	}
	handlerResp, ok := resp["handler"].(map[string]any)
	if !ok {
		t.Fatalf("resp[handler] type = %T, want map[string]any", resp["handler"])
	}
	if got, want := handlerResp["entity_id"], "content-entity:report-get"; got != want {
		t.Fatalf("handler.entity_id = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["language"], "java"; got != want {
		t.Fatalf("handler.language = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["file_path"], "src/main/java/example/ReportController.java"; got != want {
		t.Fatalf("handler.file_path = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["truth_edge"], "HANDLES_ROUTE"; got != want {
		t.Fatalf("handler.truth_edge = %#v, want %#v", got, want)
	}
}
