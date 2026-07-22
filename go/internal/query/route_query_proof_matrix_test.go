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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRouteQueryProofMatrix is the per-language route query-proof matrix
// #5361's expansion of PR #5504 added to close a codex P1 finding: PR #5504
// cited trace_route_callers as the HANDLES_ROUTE consumer for csharp, go,
// javascript, kotlin, php, python, rust, and typescript, but the only prior
// gate (TestLanguageParityReadSurfacesResolveToRealConsumers) proves the tool
// is REGISTERED, not that each language's real parser output survives
// reducer.buildHandlesRouteIntentRows's exact-only handler-name resolution
// (go/internal/reducer/handles_route_intents.go) and comes back out of
// handleRouteToCaller. Only Java (code_route_to_caller_java_test.go) proved
// the full chain; the rest is proven here, one representative framework per
// language, cribbing the handler+route shape from that language's existing
// parser route-entries test so route_entries are guaranteed to emit.
//
// Each case runs the SAME chain as the Java test: real fixture file -> real
// parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof
// -> drive handleRouteToCaller with reducer-derived fakeGraphReader rows and
// assert the handler entity/language/truth_edge come back. A non-resolving case
// can still assert the exact silent seam directly when a future language gap is
// added to the matrix.
//
// PHP covers both supported handler-token conventions. Symfony emits the
// class-qualified dotted token "ReportController.show", which exact-matches the
// candidate index. Laravel emits the idiomatic string-callable token
// "UserController@index"; resolveHandlesRouteFunction normalizes exactly one
// Class@method token to the same class-qualified dotted candidate. It never
// falls back to the bare method, so a wrong or ambiguous controller still does
// not fabricate a HANDLES_ROUTE edge (#5513).
func TestRouteQueryProofMatrix(t *testing.T) {
	t.Parallel()

	cases := []routeQueryProofCase{
		{
			language:       "go",
			framework:      "net_http",
			ecosystemDir:   "go_comprehensive",
			fixtureRelPath: "routes/handlers.go",
			handlerFn:      "ListItems",
			expectResolved: true,
		},
		{
			language:       "javascript",
			framework:      "express",
			ecosystemDir:   "javascript_comprehensive",
			fixtureRelPath: "routes/handlers.js",
			handlerFn:      "listItems",
			expectResolved: true,
		},
		{
			language:       "typescript",
			framework:      "nestjs",
			ecosystemDir:   "typescript_comprehensive",
			fixtureRelPath: "routes/accounts.controller.ts",
			handlerFn:      "getAccount",
			expectResolved: true,
		},
		{
			language:       "kotlin",
			framework:      "spring",
			ecosystemDir:   "kotlin_comprehensive",
			fixtureRelPath: "routes/Routes.kt",
			handlerFn:      "health",
			expectResolved: true,
		},
		{
			// #5395: JAX-RS, Micronaut, and Ktor share one fixture file
			// (JvmRoutes.kt), cribbed verbatim from the parser's
			// TestDefaultEngineParsePathKotlinJVMRouteSemantics fixture, so
			// all three frameworks' route_entries are guaranteed to emit
			// from a single combined parser walk.
			language:       "kotlin",
			framework:      "jax_rs",
			ecosystemDir:   "kotlin_comprehensive",
			fixtureRelPath: "routes/JvmRoutes.kt",
			handlerFn:      "show",
			expectResolved: true,
		},
		{
			language:       "kotlin",
			framework:      "micronaut",
			ecosystemDir:   "kotlin_comprehensive",
			fixtureRelPath: "routes/JvmRoutes.kt",
			handlerFn:      "health",
			expectResolved: true,
		},
		{
			language:       "kotlin",
			framework:      "ktor",
			ecosystemDir:   "kotlin_comprehensive",
			fixtureRelPath: "routes/JvmRoutes.kt",
			handlerFn:      "ping",
			expectResolved: true,
		},
		{
			language:       "python",
			framework:      "fastapi",
			ecosystemDir:   "python_comprehensive",
			fixtureRelPath: "routes/fastapi_app.py",
			handlerFn:      "health",
			expectResolved: true,
		},
		{
			language:       "rust",
			framework:      "axum",
			ecosystemDir:   "rust_comprehensive",
			fixtureRelPath: "routes/handlers.rs",
			handlerFn:      "axum_show",
			expectResolved: true,
		},
		{
			language:       "csharp",
			framework:      "aspnet",
			ecosystemDir:   "csharp_comprehensive",
			fixtureRelPath: "routes/OrdersController.cs",
			handlerFn:      "Get",
			expectResolved: true,
		},
		{
			language:       "php",
			framework:      "laravel",
			ecosystemDir:   "php_comprehensive",
			fixtureRelPath: "routes/routes.php",
			handlerFn:      "index",
			expectResolved: true,
		},
		{
			// Companion PHP case: proves PHP still has a real
			// trace_route_callers consumer. Symfony's #[Route] attribute
			// emits the class-qualified, DOT-joined handler
			// "ReportController.show", which exact-matches the "."-joined
			// candidate the index builds. Laravel's "@"-joined token above
			// reaches the same exact dotted candidate through the bounded
			// Class@method normalization.
			language:       "php",
			framework:      "symfony",
			ecosystemDir:   "php_comprehensive",
			fixtureRelPath: "routes/ReportController.php",
			handlerFn:      "show",
			expectResolved: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.language+"_"+tc.framework, func(t *testing.T) {
			t.Parallel()
			runRouteQueryProofCase(t, tc)
		})
	}
}

// routeQueryProofCase is one row of the per-language route query-proof
// matrix: one representative framework's realistic route+handler fixture,
// and whether that language's real parser output is expected to survive the
// reducer's exact-only handler resolution.
type routeQueryProofCase struct {
	language       string
	framework      string
	ecosystemDir   string
	fixtureRelPath string
	handlerFn      string
	expectResolved bool
	// seamNote documents the exact resolution seam for a non-resolving case;
	// asserted only via t.Logf so it stays discoverable in verbose test
	// output without duplicating the doc comment above.
	seamNote string
	// wantEmittedHandlerSubstring, when set on a non-resolving case, asserts
	// the parser DID emit a route entry whose handler string contains this
	// token before the reducer dropped it -- so the test proves the seam is a
	// resolution failure on a real, emitted route (e.g. Laravel's "@"-joined
	// handler) rather than the parser silently emitting no route at all.
	wantEmittedHandlerSubstring string
}

// runRouteQueryProofCase drives one routeQueryProofCase through the full
// parser -> reducer -> query chain. A resolving case asserts the handler
// entity, language, file path, and HANDLES_ROUTE truth edge come back from
// handleRouteToCaller, exactly like TestHandleRouteToCallerResolvesJavaSpringHandler.
// A non-resolving case asserts reducer.BuildHandlesRouteIntentRowsForQueryProof
// produced no HANDLES_ROUTE intent for the handler function under test, even
// though that function was really parsed and indexed -- proving the handler
// name convention itself is the reason for the silent skip, not a missing
// fixture function.
func runRouteQueryProofCase(t *testing.T, tc routeQueryProofCase) {
	t.Helper()

	repoID := "repo-" + tc.language
	handlerUID := "content-entity:" + tc.language + ":" + tc.handlerFn

	payload, relativePath := parseRouteFixtureFileForQueryProof(t, tc.ecosystemDir, tc.fixtureRelPath)
	assignQueryProofFunctionUID(t, payload, tc.handlerFn, handlerUID)
	handlerName, handlerLanguage, handlerStartLine, handlerEndLine := queryProofFunctionFields(t, payload, tc.handlerFn)

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
	intents := reducer.BuildHandlesRouteIntentRowsForQueryProof(envelopes)
	intent, resolved := findQueryProofIntentByFunctionEntityID(intents, handlerUID)

	if !tc.expectResolved {
		if resolved {
			t.Fatalf(
				"%s: expected handler %q to stay unresolved (seam: %s), but got a HANDLES_ROUTE intent: %#v",
				tc.language, tc.handlerFn, tc.seamNote, intent,
			)
		}
		// Prove the parser DID emit the route (with the seam-carrying handler
		// token) before the reducer dropped it, so this is a resolution
		// failure on a real emitted route, not silent non-emission.
		if tc.wantEmittedHandlerSubstring != "" {
			emittedHandlers := queryProofRouteEntryHandlers(payload)
			if !anyStringContains(emittedHandlers, tc.wantEmittedHandlerSubstring) {
				t.Fatalf(
					"%s: expected the parser to emit a route entry whose handler contains %q (to prove the route WAS emitted then dropped), but emitted handlers = %#v",
					tc.language, tc.wantEmittedHandlerSubstring, emittedHandlers,
				)
			}
			t.Logf("%s: parser emitted route handler(s) %#v (contains %q) -- the route was emitted, then dropped at resolution",
				tc.language, emittedHandlers, tc.wantEmittedHandlerSubstring)
		}
		t.Logf("%s: confirmed silent seam (%d intents emitted, none for %q) -- %s",
			tc.language, len(intents), handlerUID, tc.seamNote)
		return
	}

	if !resolved {
		t.Fatalf("%s: no HANDLES_ROUTE intent for %q among %d intents: %#v", tc.language, handlerUID, len(intents), intents)
	}

	routePath, _ := intent.Payload["path"].(string)
	httpMethod, _ := intent.Payload["http_method"].(string)
	framework, _ := intent.Payload["framework"].(string)
	functionEntityID, _ := intent.Payload["function_entity_id"].(string)
	if got, want := framework, tc.framework; got != want {
		t.Fatalf("%s: intent framework = %q, want %q", tc.language, got, want)
	}

	endpointID := "endpoint-" + tc.language

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
						t.Fatalf("%s: endpoint params[repo_id] = %#v, want %#v", tc.language, got, want)
					}
					if got, want := params["path"], routePath; got != want {
						t.Fatalf("%s: endpoint params[path] = %#v, want %#v", tc.language, got, want)
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
					t.Fatalf("%s: unexpected query: %s", tc.language, cypher)
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
		t.Fatalf("%s: json.Marshal(request) error = %v, want nil", tc.language, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/routes/callers", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("%s: status = %d, want %d body=%s", tc.language, got, want, w.Body.String())
	}
	if !sawRouteQuery || !sawHandlerQuery {
		t.Fatalf("%s: sawRouteQuery=%v sawHandlerQuery=%v, want both true", tc.language, sawRouteQuery, sawHandlerQuery)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s: json.Unmarshal() error = %v, want nil", tc.language, err)
	}
	if got, want := resp["status"], "complete"; got != want {
		t.Fatalf("%s: resp[status] = %#v, want %#v", tc.language, got, want)
	}
	route, ok := resp["route"].(map[string]any)
	if !ok {
		t.Fatalf("%s: resp[route] type = %T, want map[string]any", tc.language, resp["route"])
	}
	if got, want := route["framework"], framework; got != want {
		t.Fatalf("%s: route.framework = %#v, want %#v", tc.language, got, want)
	}
	handlerResp, ok := resp["handler"].(map[string]any)
	if !ok {
		t.Fatalf("%s: resp[handler] type = %T, want map[string]any", tc.language, resp["handler"])
	}
	if got, want := handlerResp["entity_id"], functionEntityID; got != want {
		t.Fatalf("%s: handler.entity_id = %#v, want %#v", tc.language, got, want)
	}
	if got, want := handlerResp["language"], handlerLanguage; got != want {
		t.Fatalf("%s: handler.language = %#v, want %#v", tc.language, got, want)
	}
	if got, want := handlerResp["file_path"], relativePath; got != want {
		t.Fatalf("%s: handler.file_path = %#v, want %#v", tc.language, got, want)
	}
	if got, want := handlerResp["truth_edge"], "HANDLES_ROUTE"; got != want {
		t.Fatalf("%s: handler.truth_edge = %#v, want %#v", tc.language, got, want)
	}
}
