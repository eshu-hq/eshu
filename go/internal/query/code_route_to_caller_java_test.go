// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestHandleRouteToCallerResolvesJavaSpringHandler proves the trace_route_callers
// query surface (handleRouteToCaller, the go_symbol/mcp_tool the java row's
// language-feature-parity ledger entry claims per #5333) resolves an
// exact-anchored Java Spring HANDLES_ROUTE edge end to end: the endpoint
// lookup, handler resolution, and bounded caller/callee traversal all run
// through the real production handler and surface Java-language route/handler
// evidence, not just the already-covered Python/fastapi shape.
//
// Continuity (#5333 review): this test does not hand-invent its
// fakeGraphReader HANDLES_ROUTE row. It parses the real java_comprehensive
// CatalogController.java fixture with the real parser.DefaultEngine(), feeds
// the JSON-round-tripped result through reducer.BuildHandlesRouteIntentRowsForQueryProof
// -- the same materialization pipeline production runs -- and builds the fake
// graph row's endpoint/handler fields entirely from that real intent output
// (function_entity_id, framework, path, http_method) and from the same parsed
// function record's own fields (name, line_number, end_line, lang). The last
// hop still uses fakeGraphReader rather than a live graph read: there is no
// graph backend available at unit-test tier, and the endpoint/Function graph
// nodes referenced by handleRouteToCaller's Cypher are materialized by a
// separate subsystem (endpoint extraction, content-entity indexing) this test
// does not re-run. What is proven is that a break at the
// parser-to-reducer-to-edge boundary changes the values fed into the fake
// row and therefore fails this test, closing the false-green gap where the
// query test's input was previously disconnected from both the parser
// fixture and the reducer's real output.
func TestHandleRouteToCallerResolvesJavaSpringHandler(t *testing.T) {
	t.Parallel()

	repoID := "repo-billing"
	endpointID := "endpoint-catalog-show"
	handlerUID := "content-entity:catalog-show"

	payload, relativePath := parseRouteFixtureFileForQueryProof(t, "java_comprehensive", filepath.Join("routes", "CatalogController.java"))
	assignQueryProofFunctionUID(t, payload, "show", handlerUID)
	handlerName, handlerLanguage, handlerStartLine, handlerEndLine := queryProofFunctionFields(t, payload, "show")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       repoID,
				"source_run_id": "run-1",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          repoID,
				"relative_path":    relativePath,
				"parsed_file_data": jsonRoundTripQueryProofPayload(t, payload),
			},
		},
	}
	// Only "show" is stamped with a content-entity uid above ("create" is
	// left un-indexed on purpose), so CatalogController.java's two real route
	// entries resolve to exactly one HANDLES_ROUTE intent: the "create"
	// entry's handler name cannot resolve to any indexed Function and is
	// skipped by resolveHandlesRouteFunction, matching production behavior
	// for a handler this test does not need.
	intents := reducer.BuildHandlesRouteIntentRowsForQueryProof(envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected 1 HANDLES_ROUTE intent from CatalogController.java, got %d: %#v", len(intents), intents)
	}
	intent, ok := findQueryProofIntentByFunctionEntityID(intents, handlerUID)
	if !ok {
		t.Fatalf("no HANDLES_ROUTE intent for %q among %#v", handlerUID, intents)
	}

	routePath, _ := intent.Payload["path"].(string)
	httpMethod, _ := intent.Payload["http_method"].(string)
	framework, _ := intent.Payload["framework"].(string)
	functionEntityID, _ := intent.Payload["function_entity_id"].(string)

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
					if got, want := params["repo_id"], repoID; got != want {
						t.Fatalf("endpoint params[repo_id] = %#v, want %#v", got, want)
					}
					if got, want := params["path"], routePath; got != want {
						t.Fatalf("endpoint params[path] = %#v, want %#v", got, want)
					}
					return []map[string]any{{
						"endpoint_id": endpointID, "path": routePath,
						"repo_id": repoID, "endpoint_framework": framework,
					}}, nil
				case isRouteToCallerHandlerQuery(cypher):
					sawHandlerQuery = true
					return []map[string]any{{
						"endpoint_id": endpointID, "http_method": httpMethod, "route_framework": framework,
						"handler_id": functionEntityID, "handler_name": handlerName,
						"handler_file_path":  relativePath,
						"handler_language":   handlerLanguage,
						"handler_start_line": int64(handlerStartLine), "handler_end_line": int64(handlerEndLine),
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

	reqBody, err := json.Marshal(map[string]any{
		"repo_id": repoID, "method": strings.ToLower(httpMethod), "path": routePath,
		"max_depth": 2, "limit": 5,
	})
	if err != nil {
		t.Fatalf("json.Marshal(request) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/routes/callers", bytes.NewReader(reqBody))
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
	if got, want := route["framework"], framework; got != want {
		t.Fatalf("route.framework = %#v, want %#v", got, want)
	}
	handlerResp, ok := resp["handler"].(map[string]any)
	if !ok {
		t.Fatalf("resp[handler] type = %T, want map[string]any", resp["handler"])
	}
	if got, want := handlerResp["entity_id"], functionEntityID; got != want {
		t.Fatalf("handler.entity_id = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["language"], handlerLanguage; got != want {
		t.Fatalf("handler.language = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["file_path"], relativePath; got != want {
		t.Fatalf("handler.file_path = %#v, want %#v", got, want)
	}
	if got, want := handlerResp["truth_edge"], "HANDLES_ROUTE"; got != want {
		t.Fatalf("handler.truth_edge = %#v, want %#v", got, want)
	}
}

// The parseRouteFixtureFileForQueryProof / assignQueryProofFunctionUID /
// queryProofFunctionFields / jsonRoundTripQueryProofPayload /
// findQueryProofIntentByFunctionEntityID helpers this test drives now live in
// route_query_proof_helpers_test.go: they are language-generic (despite this
// file's Java-specific origin) and are shared with the per-language route
// query-proof matrix in route_query_proof_matrix_test.go (#5361).
