// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// Java's framework_routes.go/spring_routes.go emit route_entries with a bare
// method-name handler (route.handler = name), not a "Class.method" qualified
// name like Scala or PHP. The three positive-path tests below parse the real
// on-disk java_comprehensive route fixtures (the same files
// TestDefaultEngineParsePathJavaComprehensiveRouteFixtures in
// internal/parser/java_comprehensive_route_fixture_test.go asserts against)
// through the real parser.DefaultEngine().ParsePath(), then round-trip the
// result through encoding/json before feeding it to the reducer. That
// round-trip is not cosmetic: mapSlice() (code_call_materialization_path_helpers.go)
// only decodes []map[string]any or []any of map[string]any, never the
// parser's raw []map[string]string route_entries shape, so the JSON
// round-trip -- which turns every JSON object into map[string]any regardless
// of its original Go type -- is the only route_entries shape the reducer can
// actually consume. It is also the shape production hits after a Postgres
// JSON round-trip. Driving these tests from the real fixture closes the
// parser-to-reducer continuity gap flagged on #5333: previously this file
// hand-invented handler names (e.g. "list") that diverged from what the
// parser fixture test actually asserted (e.g. "get"), so a parser-side
// regression could leave both tests green while production Java route
// tracing was broken.

// javaRouteFixtureRepoRoot returns the java_comprehensive ecosystem fixture
// root shared with internal/parser/java_comprehensive_route_fixture_test.go.
func javaRouteFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at <repoRoot>/go/internal/reducer/.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "tests", "fixtures", "ecosystems", "java_comprehensive")
}

// parseJavaRouteFixtureFile runs the real parser over one java_comprehensive
// route fixture file and returns its parsed_file_data payload plus the
// relative path the reducer's file envelope must carry.
func parseJavaRouteFixtureFile(t *testing.T, relPath string) (map[string]any, string) {
	t.Helper()
	repoRoot := javaRouteFixtureRepoRoot(t)
	sourcePath := filepath.Join(repoRoot, relPath)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
	}
	return payload, reducerTestRelativePath(t, repoRoot, sourcePath)
}

// assignJavaRouteFunctionUID mirrors assignReducerTestFunctionUID: it
// stamps a synthetic content-entity uid onto the real parsed function named
// name, standing in for the content-entity resolution stage that runs
// downstream of parsing in production before the reducer ever sees the fact.
func assignJavaRouteFunctionUID(t *testing.T, payload map[string]any, name string, uid string) {
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

// jsonRoundTripParsedFileData round-trips a parsed_file_data payload through
// encoding/json, turning every nested Go slice/map type into the []any /
// map[string]any shape production actually stores and the reducer actually
// reads back (see the package comment above).
func jsonRoundTripParsedFileData(t *testing.T, payload map[string]any) map[string]any {
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

func TestBuildHandlesRouteIntentRowsEmitsJavaSpringRouteMatches(t *testing.T) {
	t.Parallel()

	payload, relativePath := parseJavaRouteFixtureFile(t, filepath.Join("routes", "CatalogController.java"))
	assignJavaRouteFunctionUID(t, payload, "show", "content-entity:catalog-show")
	assignJavaRouteFunctionUID(t, payload, "create", "content-entity:catalog-create")

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		{
			FactKind: factKindFile,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":          "repo-1",
				"relative_path":    relativePath,
				"parsed_file_data": jsonRoundTripParsedFileData(t, payload),
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 2 {
		t.Fatalf("expected exactly 2 HANDLES_ROUTE intents, got %d: %#v", len(intents), intents)
	}
	sort.Slice(intents, func(i, j int) bool {
		return payloadStr(intents[i].Payload, "path") < payloadStr(intents[j].Payload, "path")
	})

	create := intents[0]
	if got, want := payloadStr(create.Payload, "function_entity_id"), "content-entity:catalog-create"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(create.Payload, "path"), "/api/catalog/items"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(create.Payload, "http_method"), "POST"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}

	show := intents[1]
	if got, want := payloadStr(show.Payload, "function_entity_id"), "content-entity:catalog-show"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(show.Payload, "framework"), "spring"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(show.Payload, "path"), "/api/catalog/items/{id}"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(show.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	if got, want := payloadStr(show.Payload, "resolution_method"), codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsJavaJAXRSRouteMatches(t *testing.T) {
	t.Parallel()

	payload, relativePath := parseJavaRouteFixtureFile(t, filepath.Join("routes", "WidgetResource.java"))
	assignJavaRouteFunctionUID(t, payload, "get", "content-entity:widget-get")

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		{
			FactKind: factKindFile,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":          "repo-1",
				"relative_path":    relativePath,
				"parsed_file_data": jsonRoundTripParsedFileData(t, payload),
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d: %#v", len(intents), intents)
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:widget-get"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "jax_rs"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/widgets/{id}"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
}

func TestBuildHandlesRouteIntentRowsEmitsJavaMicronautRouteMatches(t *testing.T) {
	t.Parallel()

	payload, relativePath := parseJavaRouteFixtureFile(t, filepath.Join("routes", "PingController.java"))
	assignJavaRouteFunctionUID(t, payload, "ping", "content-entity:micronaut-ping")

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		{
			FactKind: factKindFile,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":          "repo-1",
				"relative_path":    relativePath,
				"parsed_file_data": jsonRoundTripParsedFileData(t, payload),
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE intent, got %d: %#v", len(intents), intents)
	}
	intent := intents[0]
	if got, want := payloadStr(intent.Payload, "function_entity_id"), "content-entity:micronaut-ping"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "framework"), "micronaut"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "path"), "/ping"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
}

// TestBuildHandlesRouteIntentRowsSkipsAmbiguousJavaHandler proves java.md:80's
// documented limitation: a bare method-name handler that is not unique within
// the route file and not unique repo-wide must not produce an edge, because a
// guessed binding would corrupt graph truth (#2721).
func TestBuildHandlesRouteIntentRowsSkipsAmbiguousJavaHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/routes/Routes.java",
			nil,
			"spring",
			[]any{
				map[string]any{"method": "GET", "path": "/ambiguous", "handler": "handle"},
			},
		),
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "src/main/java/example/a/AController.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/a/AController.java",
					"functions": []map[string]any{
						{"name": "handle", "class_context": "AController", "uid": "content-entity:a-handle", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
		{
			FactKind: "file",
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"relative_path": "src/main/java/example/b/BController.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/b/BController.java",
					"functions": []map[string]any{
						{"name": "handle", "class_context": "BController", "uid": "content-entity:b-handle", "line_number": 1, "end_line": 2},
					},
				},
			},
		},
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for ambiguous Java handler, got %d: %#v", len(intents), intents)
	}
}

// TestBuildHandlesRouteIntentRowsSkipsUnknownJavaHandler proves an entry whose
// handler name resolves to zero Function entities is skipped rather than
// invented.
func TestBuildHandlesRouteIntentRowsSkipsUnknownJavaHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		handlesRouteFileEnvelope(
			"repo-1",
			"src/main/java/example/ReportController.java",
			[]map[string]any{
				{
					"name":          "get",
					"class_context": "ReportController",
					"uid":           "content-entity:report-get",
					"line_number":   14,
					"end_line":      17,
					"lang":          "java",
				},
			},
			"spring",
			[]any{
				map[string]any{"method": "GET", "path": "/api/reports/{id}", "handler": "doesNotExist"},
			},
		),
	}

	intents := buildHandlesRouteIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no HANDLES_ROUTE intent for unknown Java handler, got %d", len(intents))
	}
}
