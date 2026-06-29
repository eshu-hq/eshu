// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathCPPExactFrameworkRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "routes.cpp")
	writeTestFile(
		t,
		filePath,
		`#include <crow.h>
#include <drogon/drogon.h>
#include <pistache/router.h>

void health() {}
void createOrder() {}
void updateOrder() {}
void syncOrder() {}
void crowDynamic() {}
void drogonDynamic() {}

class OrdersController {
public:
    void show() {}
};

void registerRoutes(Pistache::Rest::Router& router) {
    CROW_ROUTE(app, "/health").methods("GET"_method)(health);
    drogon::app().registerHandler("/orders", createOrder, {drogon::Post});
    drogon::app().registerHandler("/orders/<id>", updateOrder, {drogon::Put, drogon::Patch});
    Pistache::Rest::Routes::Get(router, "/orders/:id", Pistache::Rest::Routes::bind(&OrdersController::show, this));

    CROW_ROUTE(app, dynamicPath).methods("GET"_method)(crowDynamic);
    drogon::app().registerHandler(dynamicPath, drogonDynamic, {drogon::Delete});
    Pistache::Rest::Routes::Post(router, "/inline", [](const auto&, auto) {});
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "crow", "drogon", "pistache")
	assertNestedStringSliceEqual(t, got, "crow", "route_methods", []string{"GET"})
	assertNestedStringSliceEqual(t, got, "crow", "route_paths", []string{"/health"})
	assertNestedRouteEntriesEqual(t, got, "crow", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
	})
	assertNestedStringSliceEqual(t, got, "drogon", "route_methods", []string{"POST", "PUT", "PATCH"})
	assertNestedStringSliceEqual(t, got, "drogon", "route_paths", []string{"/orders", "/orders/<id>"})
	assertNestedRouteEntriesEqual(t, got, "drogon", []map[string]string{
		{"method": "POST", "path": "/orders", "handler": "createOrder"},
		{"method": "PUT", "path": "/orders/<id>", "handler": "updateOrder"},
		{"method": "PATCH", "path": "/orders/<id>", "handler": "updateOrder"},
	})
	assertNestedStringSliceEqual(t, got, "pistache", "route_methods", []string{"GET"})
	assertNestedStringSliceEqual(t, got, "pistache", "route_paths", []string{"/orders/:id"})
	assertNestedRouteEntriesEqual(t, got, "pistache", []map[string]string{
		{"method": "GET", "path": "/orders/:id", "handler": "OrdersController.show"},
	})
}

func TestDefaultEngineParsePathCPPSkipsNonExactFrameworkRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "dynamic_routes.cpp")
	writeTestFile(
		t,
		filePath,
		`#include <crow.h>
#include <drogon/drogon.h>
#include <pistache/router.h>

void health() {}
void createOrder() {}

void registerRoutes(Pistache::Rest::Router& router) {
    CROW_ROUTE(app, "/implicit-method")(health);
    CROW_ROUTE_HELPER(app, "/helper").methods("GET"_method)(health);
    MY_CROW_ROUTE(app, "/wrapped").methods("GET"_method)(health);
    CROW_ROUTE(app, routePath).methods("GET"_method)(health);
    drogon::app().registerHandler("/orders", createOrder);
    drogon::app().registerHandlerAsync("/async", createOrder, {drogon::Get});
    localRegistry.registerHandler("/local", createOrder, {drogon::Get});
    Pistache::Rest::Routes::Get(router, routePath, createOrder);
    Pistache::Rest::Routes::Post(router, "/inline", [](const auto&, auto) {});
    Routes::Get(router, "/local-routes-namespace", createOrder);
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got)
}
