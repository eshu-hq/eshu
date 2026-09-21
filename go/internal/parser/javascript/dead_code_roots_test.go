// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestJavaScriptExpressServerSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		express map[string]any
		want    []string
	}{
		{
			name: "typed server symbols",
			express: map[string]any{
				"server_symbols": []string{"app", "router"},
			},
			want: []string{"app", "router"},
		},
		{
			name: "missing server symbols",
			express: map[string]any{
				"route_methods": []string{"GET"},
			},
			want: nil,
		},
		{
			name: "wrong server symbols shape",
			express: map[string]any{
				"server_symbols": []any{"app"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := javaScriptExpressServerSymbols(tt.express)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("javaScriptExpressServerSymbols(%#v) = %#v, want %#v", tt.express, got, tt.want)
			}
		})
	}
}

func TestDefaultEngineParsePathJavaScriptEmitsDeadCodeRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	nextPath := filepath.Join(repoRoot, "app", "api", "health", "route.ts")
	nextTSXPath := filepath.Join(repoRoot, "app", "api", "profile", "route.tsx")
	nextEnumPath := filepath.Join(repoRoot, "app", "api", "enum", "route.ts")
	expressPath := filepath.Join(repoRoot, "server", "routes.ts")
	writeTestFile(
		t,
		nextPath,
		`export async function GET() {
  return Response.json({ ok: true });
}

async function helper() {
  return Response.json({ ok: true });
}
`,
	)
	writeTestFile(
		t,
		nextTSXPath,
		`export const POST = async () => {
  return Response.json({ ok: true });
};

const localHelper = () => Response.json({ ok: true });
`,
	)
	writeTestFile(
		t,
		nextEnumPath,
		`export enum GET {
  Read = "read",
}
`,
	)
	writeTestFile(
		t,
		expressPath,
		`import express from "express";

const router = express.Router();

function login(req, res) {
  return res.send("ok");
}

const createVideo = (req, res) => res.send("ok");

function requireAuth(req, res, next) {
  return next();
}

function updateProfile(req, res) {
  return res.send("ok");
}

function arrayMiddleware(req, res, next) {
  return next();
}

function listUsers(req, res) {
  return res.send("ok");
}

router.get("/auth/login", login);
router.post("/", createVideo);
router.put("/profile", requireAuth, updateProfile);
router.get("/users", [arrayMiddleware], listUsers);
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(next) error = %v, want nil", err)
	}
	expressPayload, err := engine.ParsePath(repoRoot, expressPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(express) error = %v, want nil", err)
	}
	nextTSXPayload, err := engine.ParsePath(repoRoot, nextTSXPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(next tsx) error = %v, want nil", err)
	}
	nextEnumPayload, err := engine.ParsePath(repoRoot, nextEnumPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(next enum) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "GET"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	helperItem := assertFunctionByName(t, nextPayload, "helper")
	if _, ok := helperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-exported route helper", helperItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextTSXPayload, "POST"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localHelperItem := assertFunctionByName(t, nextTSXPayload, "localHelper")
	if _, ok := localHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for non-exported TSX route helper", localHelperItem["dead_code_root_kinds"])
	}
	enumItem := assertBucketItemByName(t, nextEnumPayload, "enums", "GET")
	if _, ok := enumItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for route enum", enumItem["dead_code_root_kinds"])
	}
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "login"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "createVideo"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "requireAuth"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "updateProfile"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "arrayMiddleware"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "listUsers"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
}

func TestDefaultEngineParsePathTypeScriptDeadCodeRootsReuseJavaScriptFamilyPolicy(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "typescript")
	expressPath := filepath.Join(repoRoot, "src", "service.ts")
	nextPath := filepath.Join(repoRoot, "src", "app", "api", "accounts", "route.ts")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	expressPayload, err := engine.ParsePath(repoRoot, expressPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(express ts) error = %v, want nil", err)
	}
	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(next ts) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, expressPayload, "saveAccount"),
		"dead_code_root_kinds",
		[]string{"javascript.express_route_registration"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "GET"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localRouteHelperItem := assertFunctionByName(t, nextPayload, "localRouteHelper")
	if _, ok := localRouteHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for TypeScript local route helper", localRouteHelperItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathTSXDeadCodeRootsReuseJavaScriptFamilyPolicy(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("deadcode", "tsx")
	nextPath := filepath.Join(repoRoot, "src", "app", "api", "profile", "route.tsx")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	nextPayload, err := engine.ParsePath(repoRoot, nextPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(next tsx) error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, nextPayload, "POST"),
		"dead_code_root_kinds",
		[]string{"javascript.nextjs_route_export"},
	)
	localRouteHelperItem := assertFunctionByName(t, nextPayload, "localRouteHelper")
	if _, ok := localRouteHelperItem["dead_code_root_kinds"]; ok {
		t.Fatalf("dead_code_root_kinds = %#v, want absent for TSX local route helper", localRouteHelperItem["dead_code_root_kinds"])
	}
}

func TestDefaultEngineParsePathJavaScriptMarksCommonJSDefaultExportClassMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "core.js")
	writeTestFile(
		t,
		filePath,
		`'use strict';

const internals = {};

exports = module.exports = internals.Core = class {
    registerServer(server) {
        this.instances.add(server);
    }

    start() {
        return true;
    }
};
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "registerServer"),
		"dead_code_root_kinds",
		[]string{"javascript.commonjs_default_export"},
	)
	assertParserStringSliceFieldValue(
		t,
		assertFunctionByName(t, got, "start"),
		"dead_code_root_kinds",
		[]string{"javascript.commonjs_default_export"},
	)
}

func TestDefaultEngineParsePathJavaScriptDoesNotRootNestedCommonJSDefaultExportClassMethods(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "factory.js")
	writeTestFile(
		t,
		filePath,
		`'use strict';

module.exports = factory(class Internal {
    cleanup() {
        return true;
    }
});
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if cleanup := assertFunctionByName(t, got, "cleanup"); cleanup["dead_code_root_kinds"] != nil {
		t.Fatalf("cleanup dead_code_root_kinds = %#v, want nil", cleanup["dead_code_root_kinds"])
	}
}
