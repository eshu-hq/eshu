// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptKoaRouteEntriesExactHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const Router = require("@koa/router");
const router = new Router();

router.get("/health", health);
router.get("namedHealth", "/named-health", namedHealth);
router.post("/orders", requireAuth, createOrder);
router.put(prefix + "/dynamic", updateOrder);
router.delete("/inline", async (ctx) => { ctx.body = "deleted"; });
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "koa")
	assertNestedStringSliceEqual(t, got, "koa", "route_methods", []string{"GET", "POST", "DELETE"})
	assertNestedStringSliceEqual(t, got, "koa", "route_paths", []string{"/health", "/named-health", "/orders", "/inline"})
	assertNestedRouteEntriesEqual(t, got, "koa", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "GET", "path": "/named-health", "handler": "namedHealth"},
		{"method": "POST", "path": "/orders"},
		{"method": "DELETE", "path": "/inline"},
	})
}

func TestDefaultEngineParsePathTypeScriptFastifyRouteEntriesExactHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "routes.ts")
	writeTestFile(
		t,
		filePath,
		`import fastify from "fastify";
const app = fastify();

app.get("/health", getHealth);
app.post("/orders", { preHandler: requireAuth }, createOrder);
app.route({ method: "PATCH", url: "/orders/:id", handler: updateOrder });
app.route({ method: ["GET", "HEAD"], url: "/multi", handler: multiMethod });
app.route({ method: methodName, url: "/dynamic-method", handler: dynamicMethod });
app.route({ method: "DELETE", url: routePath, handler: deleteOrder });
app.route({ method: "CUSTOM", url: "/custom", handler: customHandler });
app.put("/inline", async (_request, reply) => reply.send());
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "fastify")
	assertNestedStringSliceEqual(t, got, "fastify", "route_methods", []string{"GET", "POST", "PATCH", "PUT"})
	assertNestedStringSliceEqual(t, got, "fastify", "route_paths", []string{"/health", "/orders", "/orders/:id", "/inline"})
	assertNestedRouteEntriesEqual(t, got, "fastify", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "getHealth"},
		{"method": "POST", "path": "/orders", "handler": "createOrder"},
		{"method": "PATCH", "path": "/orders/:id", "handler": "updateOrder"},
		{"method": "PUT", "path": "/inline"},
	})
}

func TestDefaultEngineParsePathTypeScriptNestJSRouteEntriesExactHandlers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "accounts.controller.ts")
	writeTestFile(
		t,
		filePath,
		`import { Controller, Get, Post, Delete, All } from "@nestjs/common";

@Controller("accounts")
export class AccountsController {
  @Get(":id")
  getAccount() {
    return {};
  }

  @Post()
  createAccount() {
    return {};
  }

  @Delete(dynamicPath)
  deleteAccount() {
    return {};
  }
}

@Controller()
export class RootController {
  @All("/health")
  health() {
    return {};
  }
}

@Controller(controllerPrefix)
export class DynamicController {
  @Get("/dynamic")
  dynamic() {
    return {};
  }
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "nestjs")
	assertNestedStringSliceEqual(t, got, "nestjs", "route_methods", []string{"GET", "POST", "ANY"})
	assertNestedStringSliceEqual(t, got, "nestjs", "route_paths", []string{"/accounts/:id", "/accounts", "/health"})
	assertNestedRouteEntriesEqual(t, got, "nestjs", []map[string]string{
		{"method": "GET", "path": "/accounts/:id", "handler": "getAccount"},
		{"method": "POST", "path": "/accounts", "handler": "createAccount"},
		{"method": "ANY", "path": "/health", "handler": "health"},
	})
}
