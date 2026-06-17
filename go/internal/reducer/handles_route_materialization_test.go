package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// handlesRouteFileEnvelope builds a file fact carrying one handler function and
// one framework route entry that binds to it, for HANDLES_ROUTE fixtures.
func handlesRouteFileEnvelope(
	repoID string,
	relativePath string,
	functionName string,
	functionUID string,
	routeHandler string,
	routePath string,
	routeMethod string,
) facts.Envelope {
	routeEntry := map[string]any{
		"method": routeMethod,
		"path":   routePath,
	}
	if routeHandler != "" {
		routeEntry["handler"] = routeHandler
	}
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path": relativePath,
				"functions": []any{
					map[string]any{
						"name":        functionName,
						"line_number": 5,
						"end_line":    9,
						"uid":         functionUID,
					},
				},
				"framework_semantics": map[string]any{
					"frameworks": []any{"express"},
					"express": map[string]any{
						"route_paths":   []any{routePath},
						"route_methods": []any{routeMethod},
						"route_entries": []any{routeEntry},
					},
				},
			},
		},
	}
}

func TestExtractHandlesRouteRowsResolvesSameFileHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
			},
		},
		handlesRouteFileEnvelope(
			"repo-a", "routes.js",
			"listUsers", "entity:listUsers",
			"listUsers", "/users", "GET",
		),
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)

	if len(rows) != 1 {
		t.Fatalf("extractHandlesRouteRows rows = %d, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if got := anyToString(row["repo_id"]); got != "repo-a" {
		t.Fatalf("repo_id = %q, want repo-a", got)
	}
	if got := anyToString(row["function_id"]); got != "entity:listUsers" {
		t.Fatalf("function_id = %q, want entity:listUsers", got)
	}
	if got := anyToString(row["path"]); got != "/users" {
		t.Fatalf("path = %q, want /users", got)
	}
	if got := anyToString(row["method"]); got != "get" {
		t.Fatalf("method = %q, want get (lowercased)", got)
	}
	if got := anyToString(row["resolution_method"]); got != codeprovenance.MethodSameFile {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodSameFile)
	}
}

func TestExtractHandlesRouteRowsSkipsEntryWithoutHandler(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
			},
		},
		handlesRouteFileEnvelope(
			"repo-a", "routes.js",
			"listUsers", "entity:listUsers",
			"", "/users", "GET",
		),
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)
	if len(rows) != 0 {
		t.Fatalf("extractHandlesRouteRows rows = %d, want 0 (no handler): %+v", len(rows), rows)
	}
}

func TestExtractHandlesRouteRowsSkipsAmbiguousHandlerAcrossFiles(t *testing.T) {
	t.Parallel()

	// The same handler name appears as a function in two different files and is
	// not present in the route's own file, so it cannot be resolved to exactly
	// one Function uid. Ambiguous must stay ambiguous: zero edges.
	routeFile := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "routes.js",
			"parsed_file_data": map[string]any{
				"path":      "routes.js",
				"functions": []any{},
				"framework_semantics": map[string]any{
					"frameworks": []any{"express"},
					"express": map[string]any{
						"route_entries": []any{
							map[string]any{"method": "GET", "path": "/users", "handler": "shared"},
						},
					},
				},
			},
		},
	}
	fileOne := handlesRouteFileEnvelope("repo-a", "a.js", "shared", "entity:a-shared", "", "", "")
	fileTwo := handlesRouteFileEnvelope("repo-a", "b.js", "shared", "entity:b-shared", "", "", "")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
			},
		},
		routeFile, fileOne, fileTwo,
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)
	if len(rows) != 0 {
		t.Fatalf("extractHandlesRouteRows rows = %d, want 0 (ambiguous): %+v", len(rows), rows)
	}
}

func TestExtractHandlesRouteRowsResolvesRepoUniqueHandlerInOtherFile(t *testing.T) {
	t.Parallel()

	// Handler is defined in another file but its name is repo-unique, so it
	// resolves with the weaker repo-unique-name method.
	routeFile := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "routes.js",
			"parsed_file_data": map[string]any{
				"path":      "routes.js",
				"functions": []any{},
				"framework_semantics": map[string]any{
					"frameworks": []any{"express"},
					"express": map[string]any{
						"route_entries": []any{
							map[string]any{"method": "POST", "path": "/orders", "handler": "createOrder"},
						},
					},
				},
			},
		},
	}
	handlerFile := handlesRouteFileEnvelope("repo-a", "handlers.js", "createOrder", "entity:createOrder", "", "", "")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
			},
		},
		routeFile, handlerFile,
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)
	if len(rows) != 1 {
		t.Fatalf("extractHandlesRouteRows rows = %d, want 1: %+v", len(rows), rows)
	}
	if got := anyToString(rows[0]["function_id"]); got != "entity:createOrder" {
		t.Fatalf("function_id = %q, want entity:createOrder", got)
	}
	if got := anyToString(rows[0]["resolution_method"]); got != codeprovenance.MethodRepoUniqueName {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodRepoUniqueName)
	}
}

// handlesRouteClassRouteFileEnvelope builds a route file whose handler symbol
// names a Class (not a Function) declared in the same file, to exercise the
// non-Function resolution guards.
func handlesRouteClassRouteFileEnvelope(repoID, relativePath, className, classUID, handler, routePath, routeMethod string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":      relativePath,
				"functions": []any{},
				"classes": []any{
					map[string]any{"uid": classUID, "name": className, "line_number": 3, "end_line": 8},
				},
				"framework_semantics": map[string]any{
					"frameworks": []any{"express"},
					"express": map[string]any{
						"route_entries": []any{
							map[string]any{"method": routeMethod, "path": routePath, "handler": handler},
						},
					},
				},
			},
		},
	}
}

func TestExtractHandlesRouteRowsSkipsNonFunctionSameFileWithRepoDuplicate(t *testing.T) {
	t.Parallel()

	// The handler name resolves same-file to a Class, and a Function with the
	// same name exists in another file. The name is therefore not repo-unique,
	// so neither the same-file Function guard nor the repo-unique fallback may
	// bind it: ambiguous/non-Function stays unbound.
	routeFile := handlesRouteClassRouteFileEnvelope(
		"repo-a", "routes.js", "Handler", "cls:Handler", "Handler", "/items", "GET",
	)
	functionFile := handlesRouteFileEnvelope("repo-a", "handlers.js", "Handler", "fn:Handler", "", "", "")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-a", "source_run_id": "run-a", "graph_id": "repo-a"},
		},
		routeFile, functionFile,
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)
	if len(rows) != 0 {
		t.Fatalf("extractHandlesRouteRows rows = %d, want 0 (same-file Class + repo duplicate): %+v", len(rows), rows)
	}
}

func TestExtractHandlesRouteRowsSkipsRepoUniqueNonFunction(t *testing.T) {
	t.Parallel()

	// The handler name is repo-unique but names a Class declared in another
	// file, with no Function counterpart. The repo-unique fallback must reject
	// it because only a Function may handle a route.
	routeFile := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "routes.js",
			"parsed_file_data": map[string]any{
				"path":      "routes.js",
				"functions": []any{},
				"framework_semantics": map[string]any{
					"frameworks": []any{"express"},
					"express": map[string]any{
						"route_entries": []any{
							map[string]any{"method": "GET", "path": "/items", "handler": "MyController"},
						},
					},
				},
			},
		},
	}
	classFile := handlesRouteClassRouteFileEnvelope(
		"repo-a", "controllers.js", "MyController", "cls:MyController", "unused", "/unused", "GET",
	)

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-a", "source_run_id": "run-a", "graph_id": "repo-a"},
		},
		routeFile, classFile,
	}

	index := buildCodeEntityIndex(envelopes)
	rows := extractHandlesRouteRows(envelopes, index)
	for _, row := range rows {
		if anyToString(row["path"]) == "/items" {
			t.Fatalf("extractHandlesRouteRows bound /items to a non-Function: %+v", row)
		}
	}
}

func TestBuildHandlesRouteSharedIntentRowsProducesGatedDomain(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		{
			"repo_id":           "repo-a",
			"function_id":       "entity:listUsers",
			"path":              "/users",
			"method":            "get",
			"resolution_method": codeprovenance.MethodSameFile,
		},
	}
	contextByRepoID := map[string]ProjectionContext{
		"repo-a": {
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}

	intents := buildHandlesRouteSharedIntentRows(rows, contextByRepoID, createdAt, handlesRouteEvidenceSource)
	if len(intents) != 1 {
		t.Fatalf("intents = %d, want 1", len(intents))
	}
	intent := intents[0]
	if intent.ProjectionDomain != DomainHandlesRoute {
		t.Fatalf("ProjectionDomain = %q, want %q", intent.ProjectionDomain, DomainHandlesRoute)
	}
	if got := anyToString(intent.Payload["function_id"]); got != "entity:listUsers" {
		t.Fatalf("function_id = %q, want entity:listUsers", got)
	}
	if got := anyToString(intent.Payload["path"]); got != "/users" {
		t.Fatalf("path = %q, want /users", got)
	}
	if got := anyToString(intent.Payload["confidence"]); got == "" {
		t.Fatalf("confidence must be set from codeprovenance")
	}
	if intent.RepositoryID != "repo-a" {
		t.Fatalf("RepositoryID = %q, want repo-a", intent.RepositoryID)
	}

	// The domain must be gated at the same readiness phase as code calls
	// (canonical nodes committed), since both Function and Endpoint are canonical.
	phase, gated := sharedProjectionReadinessPhase(DomainHandlesRoute)
	if !gated {
		t.Fatalf("DomainHandlesRoute must be readiness-gated")
	}
	if phase != GraphProjectionPhaseCanonicalNodesCommitted {
		t.Fatalf("phase = %q, want %q", phase, GraphProjectionPhaseCanonicalNodesCommitted)
	}
}
