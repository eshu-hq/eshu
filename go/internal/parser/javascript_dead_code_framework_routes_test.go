package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptFrameworkRouteCallbacks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "routes.ts")
	writeTestFile(
		t,
		filePath,
		`import express from "express";
import Router from "@koa/router";

const app = express();
const router = new Router();

function requireAuth(req, res, next) {
  return next();
}

function auditRequest(ctx, next) {
  return next();
}

function expressHandler(req, res) {
  return res.send("ok");
}

function koaHandler(ctx) {
  ctx.body = "ok";
}

function localHelper() {
  return "helper";
}

app.use("/v1", requireAuth);
app.get("/health", expressHandler);
router.use(auditRequest);
router.get("/health", koaHandler);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "requireAuth"),
		"dead_code_root_kinds",
		"javascript.express_middleware_registration",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "expressHandler"),
		"dead_code_root_kinds",
		"javascript.express_route_registration",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "auditRequest"),
		"dead_code_root_kinds",
		"javascript.koa_middleware_registration",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "koaHandler"),
		"dead_code_root_kinds",
		"javascript.koa_route_registration",
	)
	if _, ok := assertFunctionByName(t, got, "localHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localHelper dead_code_root_kinds present, want absent for unregistered helper")
	}
}

func TestDefaultEngineParsePathTypeScriptFastifyFrameworkCallbacks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "server.ts")
	writeTestFile(
		t,
		filePath,
		`import fastify from "fastify";

const app = fastify();

function authHook(request, reply, done) {
  return done();
}

function healthHandler(request, reply) {
  return reply.send({ ok: true });
}

function pluginHandler(instance, opts, done) {
  return done();
}

function localHelper() {
  return "helper";
}

app.addHook("preHandler", authHook);
app.get("/health", healthHandler);
app.register(pluginHandler);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "authHook"),
		"dead_code_root_kinds",
		"javascript.fastify_hook_registration",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "healthHandler"),
		"dead_code_root_kinds",
		"javascript.fastify_route_registration",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "pluginHandler"),
		"dead_code_root_kinds",
		"javascript.fastify_plugin_registration",
	)
	if _, ok := assertFunctionByName(t, got, "localHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localHelper dead_code_root_kinds present, want absent for unregistered helper")
	}
}

func TestDefaultEngineParsePathTypeScriptFastifyRouteObjectHandler(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "server.ts")
	writeTestFile(
		t,
		filePath,
		`import fastify from "fastify";

const app = fastify();

function routeObjectHandler(request, reply) {
  return reply.send({ ok: true });
}

function localHelper() {
  return "helper";
}

app.route({
  method: "GET",
  url: "/health",
  handler: routeObjectHandler,
});
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "routeObjectHandler"),
		"dead_code_root_kinds",
		"javascript.fastify_route_registration",
	)
	if _, ok := assertFunctionByName(t, got, "localHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("localHelper dead_code_root_kinds present, want absent for unregistered helper")
	}
}

func TestDefaultEngineParsePathTypeScriptNestJSControllerMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "users.controller.ts")
	writeTestFile(
		t,
		filePath,
		`import { Controller, Get, Post } from "@nestjs/common";

@Controller("users")
export class UsersController {
  @Get()
  listUsers() {
    return [];
  }

  @Post()
  createUser() {
    return {};
  }

  helper() {
    return "helper";
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByNameAndClass(t, got, "listUsers", "UsersController"),
		"dead_code_root_kinds",
		"javascript.nestjs_controller_method",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByNameAndClass(t, got, "createUser", "UsersController"),
		"dead_code_root_kinds",
		"javascript.nestjs_controller_method",
	)
	if _, ok := assertFunctionByNameAndClass(t, got, "helper", "UsersController")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent without NestJS route decorator")
	}
}
