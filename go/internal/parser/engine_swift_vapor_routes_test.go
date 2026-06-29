// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSwiftVaporRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

func routes(_ app: Application) throws {
    app.get("health", use: health)
    app.post("widgets", use: createWidget)
    app.on(.DELETE, "widgets", ":id", use: deleteWidget)
}

func health(req: Request) async throws -> String {
    "ok"
}

func createWidget(req: Request) async throws -> String {
    "created"
}

func deleteWidget(req: Request) async throws -> HTTPStatus {
    .noContent
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNestedRouteEntriesEqual(t, got, "vapor", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "POST", "path": "/widgets", "handler": "createWidget"},
		{"method": "DELETE", "path": "/widgets/{id}", "handler": "deleteWidget"},
	})
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesLiteralGroups(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

func routes(_ app: Application) throws {
    app.group("api") { api in
        api.get("users", use: listUsers)
        api.on(.PATCH, "users", ":id", use: updateUser)
    }
    let api = ShadowRoutes()
    api.get("leaked", use: listUsers)
}

func listUsers(req: Request) async throws -> String {
    "[]"
}

func updateUser(req: Request) async throws -> HTTPStatus {
    .ok
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNestedRouteEntriesEqual(t, got, "vapor", []map[string]string{
		{"method": "GET", "path": "/api/users", "handler": "listUsers"},
		{"method": "PATCH", "path": "/api/users/{id}", "handler": "updateUser"},
	})
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesSkipOuterReceiverForGroupedDescendants(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

func routes(_ routes: RoutesBuilder) throws {
    routes.group("api") { routes in
        routes.get("users", use: listUsers)
    }
}

func listUsers(req: Request) async throws -> String {
    "[]"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNestedRouteEntriesEqual(t, got, "vapor", []map[string]string{
		{"method": "GET", "path": "/api/users", "handler": "listUsers"},
	})
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesSkipParentScopeForNestedGroups(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

func routes(_ routes: RoutesBuilder) throws {
    routes.group("api") { routes in
        routes.group("v1") { routes in
            routes.get("users", use: listUsers)
        }
    }
}

func listUsers(req: Request) async throws -> String {
    "[]"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNestedRouteEntriesEqual(t, got, "vapor", []map[string]string{
		{"method": "GET", "path": "/api/v1/users", "handler": "listUsers"},
	})
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesSkipNonExactHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

let dynamicPath = "health"

func routes(_ app: Application) throws {
    app.get(dynamicPath, use: health)
    app.get("inline") { req in
        "ok"
    }
}

func health(req: Request) async throws -> String {
    "ok"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	nested, ok := semantics["vapor"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := nested["route_entries"]; ok {
		t.Fatalf("framework_semantics.vapor.route_entries = %#v, want absent for non-exact Vapor routes", nested["route_entries"])
	}
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesSkipNonExactGroups(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

let prefix = "api"

func routes(_ app: Application) throws {
    app.group(prefix) { api in
        api.get("users", use: listUsers)
    }
    app.group("admin") { admin in
        admin.get("users") { req in
            "[]"
        }
    }
}

func scopedRoutes(_ routes: RoutesBuilder) throws {
    routes.group(prefix) { routes in
        routes.get("leaked", use: listUsers)
    }
}

func listUsers(req: Request) async throws -> String {
    "[]"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	nested, ok := semantics["vapor"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := nested["route_entries"]; ok {
		t.Fatalf("framework_semantics.vapor.route_entries = %#v, want absent for non-exact Vapor route groups", nested["route_entries"])
	}
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesRequireVaporImport(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Router.swift")
	writeTestFile(t, filePath, `func routes(_ app: CustomRouter) throws {
    app.get("health", use: health)
}

func health(req: Request) async throws -> String {
    "ok"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent without import Vapor", semantics)
	}
}

func TestDefaultEngineParsePathSwiftVaporRouteEntriesRequireRouteBuilderReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Sources", "App", "Routes.swift")
	writeTestFile(t, filePath, `import Vapor

func routes(_ cache: Cache) throws {
    cache.get("health", use: health)
}

func health(req: Request) async throws -> String {
    "ok"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for non-Vapor route receiver", semantics)
	}
}
