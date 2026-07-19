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
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
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

	payload, relativePath := parseJavaRouteFixtureFileForQueryProof(t, filepath.Join("routes", "CatalogController.java"))
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

// parseJavaRouteFixtureFileForQueryProof runs the real parser over one
// java_comprehensive route fixture file (shared with
// internal/parser/java_comprehensive_route_fixture_test.go and
// internal/reducer/handles_route_java_test.go) and returns its
// parsed_file_data payload plus the relative path a file envelope must carry.
func parseJavaRouteFixtureFileForQueryProof(t *testing.T, relPath string) (map[string]any, string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at <repoRoot>/go/internal/query/.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "tests", "fixtures", "ecosystems", "java_comprehensive")
	sourcePath := filepath.Join(repoRoot, relPath)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
	}
	relativePath, err := filepath.Rel(repoRoot, sourcePath)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) error = %v, want nil", repoRoot, sourcePath, err)
	}
	return payload, relativePath
}

// assignQueryProofFunctionUID stamps a synthetic content-entity uid onto the
// real parsed function named name, standing in for the content-entity
// resolution stage that runs downstream of parsing in production.
func assignQueryProofFunctionUID(t *testing.T, payload map[string]any, name string, uid string) {
	t.Helper()
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for i := range functions {
		if functions[i]["name"] == name {
			functions[i]["uid"] = uid
			return
		}
	}
	t.Fatalf("payload missing function %q in %#v", name, functions)
}

// queryProofFunctionFields reads the real parsed name/lang/line_number/end_line
// fields for the function named name, so the fake graph row's handler_name,
// handler_language, handler_start_line, and handler_end_line are derived from
// the same parse the reducer intent came from, not invented separately.
func queryProofFunctionFields(t *testing.T, payload map[string]any, name string) (string, string, int, int) {
	t.Helper()
	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload functions = %T, want []map[string]any", payload["functions"])
	}
	for _, fn := range functions {
		if fn["name"] != name {
			continue
		}
		lang, _ := fn["lang"].(string)
		startLine, _ := fn["line_number"].(int)
		endLine, _ := fn["end_line"].(int)
		return name, lang, startLine, endLine
	}
	t.Fatalf("payload missing function %q in %#v", name, functions)
	return "", "", 0, 0
}

// jsonRoundTripQueryProofPayload round-trips a parsed_file_data payload
// through encoding/json, the same production-realistic shape
// internal/reducer/handles_route_java_test.go round-trips before feeding the
// reducer -- []map[string]string route_entries only decode through
// mapSlice() after this round-trip turns them into []any of map[string]any.
func jsonRoundTripQueryProofPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(parsed_file_data) error = %v, want nil", err)
	}
	var roundTripped map[string]any
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal(parsed_file_data) error = %v, want nil", err)
	}
	return roundTripped
}

// findQueryProofIntentByFunctionEntityID returns the HANDLES_ROUTE intent
// whose function_entity_id matches entityID.
func findQueryProofIntentByFunctionEntityID(
	intents []reducer.SharedProjectionIntentRow, entityID string,
) (reducer.SharedProjectionIntentRow, bool) {
	for _, intent := range intents {
		if id, _ := intent.Payload["function_entity_id"].(string); id == entityID {
			return intent, true
		}
	}
	return reducer.SharedProjectionIntentRow{}, false
}
