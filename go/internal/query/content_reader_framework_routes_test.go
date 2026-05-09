package query

import (
	"encoding/json"
	"testing"
)

func TestContentReaderMetadataFixtureIsValidJSON(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"docstring":"ok","decorators":["component"],"async":true}`)
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
}

func TestParseFrameworkSemanticsExtractsHapiAndExpressRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["hapi", "express"],
		"hapi": {
			"route_methods": ["GET", "POST"],
			"route_paths": ["/elastic", "/alias/{index}/create"],
			"route_entries": [
				{"method": "GET", "path": "/elastic"},
				{"method": "POST", "path": "/alias/{index}/create"}
			],
			"server_symbols": ["server"]
		},
		"express": {
			"route_methods": ["GET"],
			"route_paths": ["/health"],
			"server_symbols": ["app"]
		}
	}`)

	results := parseFrameworkSemantics("src/routes.js", raw)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	hapi := results[0]
	if hapi.Framework != "hapi" {
		t.Fatalf("results[0].Framework = %q, want \"hapi\"", hapi.Framework)
	}
	if len(hapi.RoutePaths) != 2 {
		t.Fatalf("hapi.RoutePaths = %v, want 2 paths", hapi.RoutePaths)
	}
	if hapi.RelativePath != "src/routes.js" {
		t.Fatalf("hapi.RelativePath = %q, want \"src/routes.js\"", hapi.RelativePath)
	}
	if len(hapi.RouteEntries) != 2 {
		t.Fatalf("len(hapi.RouteEntries) = %d, want 2", len(hapi.RouteEntries))
	}
	if got, want := hapi.RouteEntries[1].Path, "/alias/{index}/create"; got != want {
		t.Fatalf("hapi.RouteEntries[1].Path = %q, want %q", got, want)
	}
	if got, want := hapi.RouteEntries[1].Method, "POST"; got != want {
		t.Fatalf("hapi.RouteEntries[1].Method = %q, want %q", got, want)
	}

	express := results[1]
	if express.Framework != "express" {
		t.Fatalf("results[1].Framework = %q, want \"express\"", express.Framework)
	}
	if len(express.RoutePaths) != 1 || express.RoutePaths[0] != "/health" {
		t.Fatalf("express.RoutePaths = %v, want [\"/health\"]", express.RoutePaths)
	}
}

func TestParseFrameworkSemanticsExtractsNextJSRouteModules(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["nextjs"],
		"nextjs": {
			"module_kind": "route",
			"route_segments": ["api", "catalog"],
			"route_verbs": ["GET", "POST"]
		}
	}`)

	results := parseFrameworkSemantics("src/app/api/catalog/route.ts", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	route := results[0]
	if route.Framework != "nextjs" {
		t.Fatalf("Framework = %q, want nextjs", route.Framework)
	}
	if len(route.RoutePaths) != 1 || route.RoutePaths[0] != "/api/catalog" {
		t.Fatalf("RoutePaths = %#v, want [/api/catalog]", route.RoutePaths)
	}
	if len(route.RouteMethods) != 2 || route.RouteMethods[0] != "GET" || route.RouteMethods[1] != "POST" {
		t.Fatalf("RouteMethods = %#v, want [GET POST]", route.RouteMethods)
	}
}

func TestParseFrameworkSemanticsSkipsEmptyFrameworks(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"frameworks": []}`)
	results := parseFrameworkSemantics("file.py", raw)
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0 for empty frameworks", len(results))
	}
}

func TestParseFrameworkSemanticsSkipsFrameworkWithNoRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["fastapi"],
		"fastapi": {
			"route_methods": ["GET"],
			"route_paths": [],
			"server_symbols": ["app"]
		}
	}`)

	results := parseFrameworkSemantics("api/main.py", raw)
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0 for framework with empty route_paths", len(results))
	}
}
