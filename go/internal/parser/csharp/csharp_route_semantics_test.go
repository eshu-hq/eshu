// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathCSharpASPNetAttributeRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Controllers", "OrdersController.cs")
	writeCSharpTestFile(
		t,
		filePath,
		`using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/orders")]
public sealed class OrdersController : ControllerBase {
    [HttpGet("{id}")]
    public string Get(string id) => id;

    [HttpPost("search", Name = "SearchOrders")]
    public string Search() => "ok";

    [HttpDelete]
    public string Delete() => "deleted";

    [HttpGet(Name = "NamedOnly")]
    public string NamedOnly() => "ok";

    [HttpGet(DynamicRoute)]
    public string Dynamic() => "skip";

    [NonAction]
    [HttpGet("helper")]
    public string Helper() => "skip";
}

public sealed class ConventionOnlyController : ControllerBase {
    public string Index() => "skip";
}

public sealed class HealthProbe {
    [HttpGet("health")]
    public string Health() => "skip";
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "aspnet")
	parsertest.AssertNestedStringSliceEqual(t, got, "aspnet", "route_methods", []string{"GET", "POST", "DELETE"})
	parsertest.AssertNestedStringSliceEqual(t, got, "aspnet", "route_paths", []string{"/api/orders/{id}", "/api/orders/search", "/api/orders"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "aspnet", []map[string]string{
		{"method": "GET", "path": "/api/orders/{id}", "handler": "OrdersController.Get"},
		{"method": "POST", "path": "/api/orders/search", "handler": "OrdersController.Search"},
		{"method": "DELETE", "path": "/api/orders", "handler": "OrdersController.Delete"},
		{"method": "GET", "path": "/api/orders", "handler": "OrdersController.NamedOnly"},
	})
}

func TestDefaultEngineParsePathCSharpASPNetMinimalAPIRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Program.cs")
	writeCSharpTestFile(
		t,
		filePath,
		`using Microsoft.AspNetCore.Builder;

var app = WebApplication.CreateBuilder(args).Build();

app.MapGet("/health", Health);
app.MapPost("/orders", CreateOrder);
app.MapMethods("/sync", new[] { "PUT", "PATCH" }, Sync);
app.MapGet(dynamicPath, Dynamic);
app.MapDelete("/inline", () => Results.Ok());

static string Health() => "ok";
static string CreateOrder() => "created";
static string Sync() => "synced";
static string Dynamic() => "skip";
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "aspnet_minimal_api")
	parsertest.AssertNestedStringSliceEqual(t, got, "aspnet_minimal_api", "route_methods", []string{"GET", "POST", "PUT", "PATCH"})
	parsertest.AssertNestedStringSliceEqual(t, got, "aspnet_minimal_api", "route_paths", []string{"/health", "/orders", "/sync"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "aspnet_minimal_api", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "Health"},
		{"method": "POST", "path": "/orders", "handler": "CreateOrder"},
		{"method": "PUT", "path": "/sync", "handler": "Sync"},
		{"method": "PATCH", "path": "/sync", "handler": "Sync"},
	})
}
