package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

// These tests pin the tree-sitter AST extraction that replaced the previous
// regex/string scanning in the JavaScript-family parser. They cover exports,
// require/import edges, re-exports (named and star), default exports, dynamic
// import, CommonJS versus ESM, and TSX so a regression in the AST walk is caught
// at the engine boundary rather than only by the comprehensive golden fixtures.

func TestDefaultEngineParsePathTSXReExportsNamedAndStar(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "barrel.tsx")
	writeTestFile(
		t,
		filePath,
		`export { Button } from "./button";
export { Card as Panel } from "./card";
export * from "./icons";
export { default as Logo } from "./logo";
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	button := findNamedBucketItem(t, got, "imports", "Button")
	assertStringFieldValue(t, button, "source", "./button")
	assertStringFieldValue(t, button, "import_type", "reexport")

	panel := findNamedBucketItem(t, got, "imports", "Panel")
	assertStringFieldValue(t, panel, "source", "./card")
	assertStringFieldValue(t, panel, "original_name", "Card")

	star := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, star, "source", "./icons")
	assertStringFieldValue(t, star, "import_type", "reexport")

	logo := findNamedBucketItem(t, got, "imports", "Logo")
	assertStringFieldValue(t, logo, "source", "./logo")
	assertStringFieldValue(t, logo, "original_name", "default")
}

func TestDefaultEngineParsePathCommonJSRequireEdges(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.js")
	writeTestFile(
		t,
		filePath,
		`const path = require("path");
const { readFile, writeFile: write } = require("fs");
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	pathImport := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, pathImport, "source", "path")
	assertStringFieldValue(t, pathImport, "import_type", "require")
	assertStringFieldValue(t, pathImport, "alias", "path")

	readFile := findNamedBucketItem(t, got, "imports", "readFile")
	assertStringFieldValue(t, readFile, "source", "fs")
	assertStringFieldValue(t, readFile, "import_type", "require")

	write := findNamedBucketItem(t, got, "imports", "writeFile")
	assertStringFieldValue(t, write, "source", "fs")
	assertStringFieldValue(t, write, "alias", "write")
}

func TestDefaultEngineParsePathESMDefaultAndDynamicImport(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "loader.ts")
	writeTestFile(
		t,
		filePath,
		`import express from "express";
import * as utils from "./utils";

export default function createApp() {
  return express();
}

async function lazy() {
  const mod = await import("./feature");
  return mod;
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	expressImport := findNamedBucketItem(t, got, "imports", "default")
	assertStringFieldValue(t, expressImport, "source", "express")
	assertStringFieldValue(t, expressImport, "alias", "express")

	utilsImport := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, utilsImport, "source", "./utils")
	assertStringFieldValue(t, utilsImport, "alias", "utils")

	assertNamedBucketContains(t, got, "functions", "createApp")
	assertNamedBucketContains(t, got, "functions", "lazy")
}

func TestDefaultEngineParsePathMethodKindsFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "shape.ts")
	writeTestFile(
		t,
		filePath,
		`class Shape {
  get area() {
    return 1;
  }

  set area(value: number) {
    this._area = value;
  }

  async load() {
    return Promise.resolve(1);
  }

  *iterate() {
    yield 1;
  }

  static async *stream() {
    yield 1;
  }
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertStringFieldValue(t, findNamedBucketItem(t, got, "functions", "area"), "type", "getter")
	assertStringFieldValue(t, findNamedBucketItem(t, got, "functions", "load"), "type", "async")
	assertStringFieldValue(t, findNamedBucketItem(t, got, "functions", "iterate"), "type", "generator")
	// static async *stream classifies as async, matching the prior precedence
	// where a leading async wins over the generator star.
	assertStringFieldValue(t, findNamedBucketItem(t, got, "functions", "stream"), "type", "async")
}

func TestDefaultEngineParsePathEmbeddedShellESMAndNodePrefix(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "runner.js")
	writeTestFile(
		t,
		filePath,
		`import { execSync } from "node:child_process";
import * as proc from "child_process";

function build() {
  execSync("make");
}

function deploy() {
  proc.spawn("kubectl");
}

function shadow() {
  const proc = {};
  proc.spawn("noop");
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	commands, ok := got["embedded_shell_commands"].([]map[string]any)
	if !ok {
		t.Fatalf("embedded_shell_commands = %T, want []map[string]any", got["embedded_shell_commands"])
	}
	want := []map[string]any{
		{
			"function_name":        "build",
			"function_line_number": 4,
			"line_number":          5,
			"api":                  "child_process.execSync",
			"language":             "javascript",
		},
		{
			"function_name":        "deploy",
			"function_line_number": 8,
			"line_number":          9,
			"api":                  "child_process.spawn",
			"language":             "javascript",
		},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("embedded_shell_commands = %#v, want %#v", commands, want)
	}
}

func TestDefaultEngineParsePathExpressESMRoutesFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "api.ts")
	writeTestFile(
		t,
		filePath,
		`import express from "express";
const app = express();

app.get("/users", listUsers);
app.post("/users", (req, res) => res.end());
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertFrameworksEqual(t, got, "express")
	assertNestedStringSliceEqual(t, got, "express", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "express", "route_paths", []string{"/users"})
	assertNestedRouteEntriesEqual(t, got, "express", []map[string]string{
		{"method": "GET", "path": "/users", "handler": "listUsers"},
		{"method": "POST", "path": "/users"},
	})
	assertNestedStringSliceEqual(t, got, "express", "server_symbols", []string{"app"})
}

func TestDefaultEngineParsePathHapiRoutesNestedConfigFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "routes.js")
	writeTestFile(
		t,
		filePath,
		`const Hapi = require("@hapi/hapi");

server.route({ method: "GET", path: "/ping", handler: ping });
server.route({
  method: "POST",
  path: "/echo",
  config: {
    handler: echo
  }
});
server.route({ method: "PUT", path: "/inline", handler: (req, h) => h.response() });
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertFrameworksEqual(t, got, "hapi")
	assertNestedStringSliceEqual(t, got, "hapi", "route_methods", []string{"GET", "POST", "PUT"})
	assertNestedStringSliceEqual(t, got, "hapi", "route_paths", []string{"/ping", "/echo", "/inline"})
	assertNestedRouteEntriesEqual(t, got, "hapi", []map[string]string{
		{"method": "GET", "path": "/ping", "handler": "ping"},
		{"method": "POST", "path": "/echo", "handler": "echo"},
		{"method": "PUT", "path": "/inline"},
	})
}

func TestDefaultEngineParsePathCloudClientImportSlugsFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "clients.ts")
	writeTestFile(
		t,
		filePath,
		`import { DynamoDBClient } from "@aws-sdk/client-dynamodb";
import { RDSDataClient } from "@aws-sdk/client-rds-data";
const storage = require("@google-cloud/storage");
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertFrameworksEqual(t, got, "aws", "gcp")
	// rds-data keeps only the trailing dash segment, matching the prior slug rule.
	assertNestedStringSliceEqual(t, got, "aws", "services", []string{"dynamodb", "data"})
	assertNestedStringSliceEqual(t, got, "gcp", "services", []string{"storage"})
}

func TestDefaultEngineParsePathNextJSRouteSurfaceFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app", "api", "items", "route.ts")
	writeTestFile(
		t,
		filePath,
		`"use server";

export const metadata = { title: "Items" };

export async function GET() {
  return new Response("ok");
}

export function POST() {
  return new Response("created");
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertNestedStringValue(t, got, "nextjs", "module_kind", "route")
	assertNestedStringValue(t, got, "nextjs", "metadata_exports", "static")
	assertNestedStringValue(t, got, "nextjs", "runtime_boundary", "server")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_verbs", []string{"GET", "POST"})
}

func TestDefaultEngineParsePathJSXReturnComponentFromAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "components", "Banner.jsx")
	writeTestFile(
		t,
		filePath,
		`export function Banner() {
  return <header>Hello</header>;
}

export const Badge = () => <span>new</span>;
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertNamedBucketContains(t, got, "components", "Banner")
	assertNamedBucketContains(t, got, "components", "Badge")
}

// TestDefaultEngineParsePathReactHookMemberCallParity pins that hook detection
// keeps the legitimate matches the prior raw-source regex produced (a namespaced
// member call such as React.useState(...)) while dropping the regex's false
// positives over comment and string content. The old regex
// `\b(use[A-Z][A-Za-z0-9_]*)\s*\(` matched React.useState( and also any
// use...( token sitting inside a comment or string literal; the AST walk must
// match the real call and ignore the non-code occurrences.
func TestDefaultEngineParsePathReactHookMemberCallParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "components", "Widget.tsx")
	writeTestFile(
		t,
		filePath,
		`import React from "react";

// useGhost( should never be collected from this comment
const note = "useString( must not be collected from a string literal";

export function Widget() {
  const [open] = React.useState(false);
  useToolbarOverflow();
  return <div>{String(open)}</div>;
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertNestedStringSliceEqual(t, got, "react", "hooks_used", []string{"useState", "useToolbarOverflow"})
}

// TestDefaultEngineParsePathAWSClientSymbolConstructorOnly pins that the AWS
// client_symbols bucket reports only symbols actually constructed with new, not
// every XxxClient token. The prior raw-source regex `\b([A-Z][A-Za-z0-9]+Client)\b`
// also matched the import binding, type annotations, and comment text, which
// over-reported symbols the file never instantiates. The AST walk fixes that:
// here S3Client is imported and annotated but never constructed, while SSMClient
// is constructed, so only SSMClient is reported.
func TestDefaultEngineParsePathAWSClientSymbolConstructorOnly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aws", "clients.ts")
	writeTestFile(
		t,
		filePath,
		`import { S3Client } from "@aws-sdk/client-s3";
import { SSMClient } from "@aws-sdk/client-ssm";

// GhostClient referenced only in this comment must be ignored.
let pending: S3Client;

export function makeParamStore() {
  return new SSMClient({ region: "us-east-1" });
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertNestedStringSliceEqual(t, got, "aws", "client_symbols", []string{"SSMClient"})
}

// TestDefaultEngineParsePathAWSServiceDynamicImportParity pins that the AWS
// services bucket still recognizes a service imported through a dynamic
// import("@aws-sdk/client-*") call. The prior raw-source regex matched the
// specifier string regardless of import form; the AST specifier collector must
// cover static import, require(), and dynamic import() so this real form is not
// silently dropped.
func TestDefaultEngineParsePathAWSServiceDynamicImportParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aws", "lazy.ts")
	writeTestFile(
		t,
		filePath,
		`export async function load() {
  const mod = await import("@aws-sdk/client-s3");
  return mod;
}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)
	assertNestedStringSliceEqual(t, got, "aws", "services", []string{"s3"})
}

func mustParsePath(t *testing.T, repoRoot string, filePath string) map[string]any {
	t.Helper()
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	return got
}
