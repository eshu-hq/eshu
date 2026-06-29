// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestParseFrameworkSemanticsSurfacesRouteHandlerSymbol(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["express"],
		"express": {
			"route_methods": ["GET", "POST"],
			"route_paths": ["/widgets", "/widgets/inline"],
			"route_entries": [
				{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
				{"method": "POST", "path": "/widgets/inline"}
			],
			"server_symbols": ["app"]
		}
	}`)

	results := parseFrameworkSemantics("src/routes.js", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	express := results[0]
	if len(express.RouteEntries) != 2 {
		t.Fatalf("len(express.RouteEntries) = %d, want 2", len(express.RouteEntries))
	}
	if got, want := express.RouteEntries[0].Handler, "getWidgets"; got != want {
		t.Fatalf("RouteEntries[0].Handler = %q, want %q", got, want)
	}
	// An ambiguous (inline/middleware) route carries no handler symbol; it must
	// stay empty rather than fabricate a binding.
	if got := express.RouteEntries[1].Handler; got != "" {
		t.Fatalf("RouteEntries[1].Handler = %q, want empty for an unbound route", got)
	}
}

func TestParseFrameworkSemanticsExtractsSwiftVaporGroupedRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["vapor"],
		"vapor": {
			"route_methods": ["GET", "PATCH"],
			"route_paths": ["/api/users", "/api/users/{id}"],
			"route_entries": [
				{"method": "GET", "path": "/api/users", "handler": "listUsers"},
				{"method": "PATCH", "path": "/api/users/{id}", "handler": "updateUser"}
			]
		}
	}`)

	results := parseFrameworkSemantics("Sources/App/Routes.swift", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	vapor := results[0]
	if got, want := vapor.Framework, "vapor"; got != want {
		t.Fatalf("Framework = %q, want %q", got, want)
	}
	if got, want := vapor.RelativePath, "Sources/App/Routes.swift"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if len(vapor.RouteEntries) != 2 {
		t.Fatalf("len(RouteEntries) = %d, want 2", len(vapor.RouteEntries))
	}
	if got, want := vapor.RouteEntries[0].Path, "/api/users"; got != want {
		t.Fatalf("RouteEntries[0].Path = %q, want %q", got, want)
	}
	if got, want := vapor.RouteEntries[0].Handler, "listUsers"; got != want {
		t.Fatalf("RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := vapor.RouteEntries[1].Method, "PATCH"; got != want {
		t.Fatalf("RouteEntries[1].Method = %q, want %q", got, want)
	}
	if got, want := vapor.RouteEntries[1].Path, "/api/users/{id}"; got != want {
		t.Fatalf("RouteEntries[1].Path = %q, want %q", got, want)
	}
}

func TestParseFrameworkSemanticsExtractsGoFrameworkRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["net_http", "gin", "echo", "chi", "fiber"],
		"net_http": {
			"route_methods": ["GET"],
			"route_paths": ["/net"],
			"route_entries": [{"method": "GET", "path": "/net", "handler": "NetHTTP"}]
		},
		"gin": {
			"route_methods": ["GET"],
			"route_paths": ["/gin"],
			"route_entries": [{"method": "GET", "path": "/gin", "handler": "Gin"}]
		},
		"echo": {
			"route_methods": ["GET"],
			"route_paths": ["/echo"],
			"route_entries": [{"method": "GET", "path": "/echo", "handler": "Echo"}]
		},
		"chi": {
			"route_methods": ["PATCH"],
			"route_paths": ["/chi/{id}"],
			"route_entries": [{"method": "PATCH", "path": "/chi/{id}", "handler": "Chi"}]
		},
		"fiber": {
			"route_methods": ["POST"],
			"route_paths": ["/fiber"],
			"route_entries": [{"method": "POST", "path": "/fiber", "handler": "Fiber"}]
		}
	}`)

	results := parseFrameworkSemantics("cmd/server/routes.go", raw)
	if len(results) != 5 {
		t.Fatalf("len(results) = %d, want 5", len(results))
	}
	wantHandlers := map[string]string{
		"net_http": "NetHTTP",
		"gin":      "Gin",
		"echo":     "Echo",
		"chi":      "Chi",
		"fiber":    "Fiber",
	}
	for _, route := range results {
		if route.RelativePath != "cmd/server/routes.go" {
			t.Fatalf("RelativePath = %q, want cmd/server/routes.go", route.RelativePath)
		}
		if len(route.RouteEntries) != 1 {
			t.Fatalf("%s RouteEntries = %#v, want exactly one entry", route.Framework, route.RouteEntries)
		}
		want, ok := wantHandlers[route.Framework]
		if !ok {
			t.Fatalf("unexpected framework %q in results %#v", route.Framework, results)
		}
		if got := route.RouteEntries[0].Handler; got != want {
			t.Fatalf("%s handler = %q, want %q", route.Framework, got, want)
		}
	}
}

func TestParseFrameworkSemanticsExtractsJavaScriptFrameworkRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["koa", "fastify", "nestjs"],
		"koa": {
			"route_methods": ["GET"],
			"route_paths": ["/koa"],
			"route_entries": [{"method": "GET", "path": "/koa", "handler": "Koa"}]
		},
		"fastify": {
			"route_methods": ["POST"],
			"route_paths": ["/fastify"],
			"route_entries": [{"method": "POST", "path": "/fastify", "handler": "Fastify"}]
		},
		"nestjs": {
			"route_methods": ["PATCH"],
			"route_paths": ["/nest/:id"],
			"route_entries": [{"method": "PATCH", "path": "/nest/:id", "handler": "Nest"}]
		}
	}`)

	results := parseFrameworkSemantics("src/routes.ts", raw)
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	wantHandlers := map[string]string{
		"koa":     "Koa",
		"fastify": "Fastify",
		"nestjs":  "Nest",
	}
	for _, route := range results {
		if route.RelativePath != "src/routes.ts" {
			t.Fatalf("RelativePath = %q, want src/routes.ts", route.RelativePath)
		}
		if len(route.RouteEntries) != 1 {
			t.Fatalf("%s RouteEntries = %#v, want exactly one entry", route.Framework, route.RouteEntries)
		}
		want, ok := wantHandlers[route.Framework]
		if !ok {
			t.Fatalf("unexpected framework %q in results %#v", route.Framework, results)
		}
		if got := route.RouteEntries[0].Handler; got != want {
			t.Fatalf("%s handler = %q, want %q", route.Framework, got, want)
		}
	}
}

func TestParseFrameworkSemanticsExtractsCSharpASPNetRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["aspnet", "aspnet_minimal_api"],
		"aspnet": {
			"route_methods": ["GET"],
			"route_paths": ["/api/orders/{id}"],
			"route_entries": [{"method": "GET", "path": "/api/orders/{id}", "handler": "OrdersController.Get"}]
		},
		"aspnet_minimal_api": {
			"route_methods": ["POST"],
			"route_paths": ["/orders"],
			"route_entries": [{"method": "POST", "path": "/orders", "handler": "CreateOrder"}]
		}
	}`)

	results := parseFrameworkSemantics("Controllers/OrdersController.cs", raw)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	wantHandlers := map[string]string{
		"aspnet":             "OrdersController.Get",
		"aspnet_minimal_api": "CreateOrder",
	}
	for _, route := range results {
		if route.RelativePath != "Controllers/OrdersController.cs" {
			t.Fatalf("RelativePath = %q, want Controllers/OrdersController.cs", route.RelativePath)
		}
		if len(route.RouteEntries) != 1 {
			t.Fatalf("%s RouteEntries = %#v, want exactly one entry", route.Framework, route.RouteEntries)
		}
		want, ok := wantHandlers[route.Framework]
		if !ok {
			t.Fatalf("unexpected framework %q in results %#v", route.Framework, results)
		}
		if got := route.RouteEntries[0].Handler; got != want {
			t.Fatalf("%s handler = %q, want %q", route.Framework, got, want)
		}
	}
}

func TestParseFrameworkSemanticsExtractsCPPFrameworkRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["crow", "drogon", "pistache"],
		"crow": {
			"route_methods": ["GET"],
			"route_paths": ["/health"],
			"route_entries": [{"method": "GET", "path": "/health", "handler": "health"}]
		},
		"drogon": {
			"route_methods": ["POST"],
			"route_paths": ["/orders"],
			"route_entries": [{"method": "POST", "path": "/orders", "handler": "createOrder"}]
		},
		"pistache": {
			"route_methods": ["GET"],
			"route_paths": ["/orders/:id"],
			"route_entries": [{"method": "GET", "path": "/orders/:id", "handler": "OrdersController.show"}]
		}
	}`)

	results := parseFrameworkSemantics("src/routes.cpp", raw)
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	wantHandlers := map[string]string{
		"crow":     "health",
		"drogon":   "createOrder",
		"pistache": "OrdersController.show",
	}
	for _, route := range results {
		if route.RelativePath != "src/routes.cpp" {
			t.Fatalf("RelativePath = %q, want src/routes.cpp", route.RelativePath)
		}
		if len(route.RouteEntries) != 1 {
			t.Fatalf("%s RouteEntries = %#v, want exactly one entry", route.Framework, route.RouteEntries)
		}
		want, ok := wantHandlers[route.Framework]
		if !ok {
			t.Fatalf("unexpected framework %q in results %#v", route.Framework, results)
		}
		if got := route.RouteEntries[0].Handler; got != want {
			t.Fatalf("%s handler = %q, want %q", route.Framework, got, want)
		}
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

func TestParseFrameworkSemanticsSurfacesNextJSRouteEntries(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["nextjs"],
		"nextjs": {
			"module_kind": "route",
			"route_segments": ["api", "catalog"],
			"route_verbs": ["GET", "POST"],
			"route_entries": [
				{"method": "GET", "path": "/api/catalog", "handler": "GET"},
				{"method": "POST", "path": "/api/catalog", "handler": "POST"}
			]
		}
	}`)

	results := parseFrameworkSemantics("src/app/api/catalog/route.ts", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	route := results[0]
	if len(route.RouteEntries) != 2 {
		t.Fatalf("len(RouteEntries) = %d, want 2", len(route.RouteEntries))
	}
	if got, want := route.RouteEntries[0].Handler, "GET"; got != want {
		t.Fatalf("RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := route.RouteEntries[1].Method, "POST"; got != want {
		t.Fatalf("RouteEntries[1].Method = %q, want %q", got, want)
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
