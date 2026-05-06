package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptSemanticsAndTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "api", "health", "route.ts")
	writeTestFile(
		t,
		filePath,
		`import { NextRequest, NextResponse } from "next/server";
import express from "express";
import { SSMClient } from "@aws-sdk/client-ssm";

enum Direction {
  Up = "UP",
}

type Handler = (value: string) => string;

const app = express();
const client = new SSMClient({ region: "us-east-1" });

app.get("/health", (_req, _res) => {
  return null;
});

export async function GET(_request: NextRequest) {
  return NextResponse.json({ ok: true });
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

	assertNamedBucketContains(t, got, "enums", "Direction")
	assertNamedBucketContains(t, got, "type_aliases", "Handler")
	assertFrameworksEqual(t, got, "nextjs", "express", "aws")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_verbs", []string{"GET"})
	assertNestedStringSliceEqual(t, got, "nextjs", "route_segments", []string{"api", "health"})
	assertNestedStringValue(t, got, "nextjs", "module_kind", "route")
	assertNestedStringValue(t, got, "nextjs", "runtime_boundary", "server")
	assertNestedStringSliceEqual(
		t,
		got,
		"nextjs",
		"request_response_apis",
		[]string{"NextRequest", "NextResponse"},
	)
	assertNestedStringSliceEqual(t, got, "express", "route_methods", []string{"GET"})
	assertNestedStringSliceEqual(t, got, "express", "route_paths", []string{"/health"})
	assertNestedStringSliceEqual(t, got, "express", "server_symbols", []string{"app"})
	assertNestedStringSliceEqual(t, got, "aws", "services", []string{"ssm"})
	assertNestedStringSliceEqual(t, got, "aws", "client_symbols", []string{"SSMClient"})
}

func TestDefaultEngineParsePathTSXSemanticsAndComponents(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "app", "[locale]", "catalog", "page.tsx")
	writeTestFile(
		t,
		filePath,
		`"use client";

import React, { useState } from "react";
import { useToolbarOverflow } from "./hooks/useToolbarOverflow";
import type { Metadata } from "next";

type User = {
  id: string;
};

export async function generateMetadata(): Promise<Metadata> {
  return { title: "Catalog" };
}

export default function CatalogPage() {
  const [open, setOpen] = useState(false);
  useToolbarOverflow();
  return <button onClick={() => setOpen(!open)}>{String(open)}</button>;
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "type_aliases", "User")
	assertNamedBucketContains(t, got, "components", "CatalogPage")
	assertFrameworksEqual(t, got, "nextjs", "react")
	assertNestedStringValue(t, got, "react", "boundary", "client")
	assertNestedStringSliceEqual(t, got, "react", "component_exports", []string{"CatalogPage"})
	assertNestedStringSliceEqual(t, got, "react", "hooks_used", []string{"useState", "useToolbarOverflow"})
	assertNestedStringValue(t, got, "nextjs", "module_kind", "page")
	assertNestedStringValue(t, got, "nextjs", "metadata_exports", "dynamic")
	assertNestedStringSliceEqual(t, got, "nextjs", "route_segments", []string{"[locale]", "catalog"})
	assertNestedStringValue(t, got, "nextjs", "runtime_boundary", "client")
}

func TestDefaultEngineParsePathJavaScriptFrameworkSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.jsx")
	writeTestFile(
		t,
		filePath,
		`"use client";

const express = require("express");
const vision = require("@google-cloud/vision");
const router = express.Router();

export function ToolbarButton() {
  const [open, setOpen] = useState(false);
  useToolbarOverflow();
  return open;
}

router.get("/auth/login", login);
router.post("/", createVideo);

const client = new vision.ImageAnnotatorClient();
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

	assertFrameworksEqual(t, got, "express", "gcp", "react")
	assertNestedStringSliceEqual(t, got, "express", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "express", "route_paths", []string{"/auth/login", "/"})
	assertNestedRouteEntriesEqual(t, got, "express", []map[string]string{
		{"method": "GET", "path": "/auth/login"},
		{"method": "POST", "path": "/"},
	})
	assertNestedStringSliceEqual(t, got, "express", "server_symbols", []string{"router"})
	assertNestedStringSliceEqual(t, got, "gcp", "services", []string{"vision"})
	assertNestedStringSliceEqual(t, got, "gcp", "client_symbols", []string{"ImageAnnotatorClient"})
	assertNestedStringValue(t, got, "react", "boundary", "client")
	assertNestedStringSliceEqual(t, got, "react", "component_exports", []string{"ToolbarButton"})
	assertNestedStringSliceEqual(t, got, "react", "hooks_used", []string{"useState", "useToolbarOverflow"})
}

func TestDefaultEngineParsePathJavaScriptHapiRouteEntriesPreserveMethodPathPairs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const Hapi = require("@hapi/hapi");

module.exports = function registerRoutes(server) {
  server.route([
    { method: "GET", path: "/health", handler: health },
    { path: "/orders/{id}", method: "POST", handler: createOrder },
    { method: "DELETE", path: "/orders/{id}", handler: deleteOrder },
    {
      method: "PUT",
      path: "/orders/{id}/metadata",
      config: {
        handler: updateMetadata,
        auth: "default"
      }
    },
  ]);
};
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

	assertFrameworksEqual(t, got, "hapi")
	assertNestedStringSliceEqual(t, got, "hapi", "route_methods", []string{"GET", "POST", "DELETE", "PUT"})
	assertNestedStringSliceEqual(t, got, "hapi", "route_paths", []string{"/health", "/orders/{id}", "/orders/{id}/metadata"})
	assertNestedRouteEntriesEqual(t, got, "hapi", []map[string]string{
		{"method": "GET", "path": "/health"},
		{"method": "POST", "path": "/orders/{id}"},
		{"method": "DELETE", "path": "/orders/{id}"},
		{"method": "PUT", "path": "/orders/{id}/metadata"},
	})
}

func TestDefaultEngineParsePathJavaScriptDocstringsAndMethodKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "runtime_surface.js")
	writeTestFile(
		t,
		filePath,
		`/** Documented utility. */
function documented(value) {
  return value;
}

class Counter {
  get count() {
    return 1;
  }

  set count(value) {
    this._count = value;
  }

  async load() {
    return Promise.resolve(this.count);
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

	documented := findNamedBucketItem(t, got, "functions", "documented")
	assertStringFieldValue(t, documented, "docstring", "Documented utility.")

	countItems := findAllNamedBucketItems(t, got, "functions", "count")
	if len(countItems) != 2 {
		t.Fatalf("functions.count entries = %#v, want 2 items", countItems)
	}
	assertStringFieldValue(t, countItems[0], "type", "getter")
	assertStringFieldValue(t, countItems[1], "type", "setter")

	load := findNamedBucketItem(t, got, "functions", "load")
	assertStringFieldValue(t, load, "type", "async")
}

func TestDefaultEngineParsePathJavaScriptGeneratorFunctions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "generator.js")
	writeTestFile(
		t,
		filePath,
		`function* createIds() {
  yield 1;
}

const buildIds = function* buildIds() {
  yield 2;
};

class Registry {
  *iterate() {
    yield 3;
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

	createIDs := findNamedBucketItem(t, got, "functions", "createIds")
	assertStringFieldValue(t, createIDs, "type", "generator")
	assertStringFieldValue(t, createIDs, "semantic_kind", "generator")

	buildIDs := findNamedBucketItem(t, got, "functions", "buildIds")
	assertStringFieldValue(t, buildIDs, "type", "generator")
	assertStringFieldValue(t, buildIDs, "semantic_kind", "generator")

	iterate := findNamedBucketItem(t, got, "functions", "iterate")
	assertStringFieldValue(t, iterate, "type", "generator")
	assertStringFieldValue(t, iterate, "semantic_kind", "generator")
}

func TestDefaultEngineParsePathJSXStatelessComponentSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "components", "HeroBanner.jsx")
	writeTestFile(
		t,
		filePath,
		`export function HeroBanner() {
  return <section className="hero">Catalog</section>;
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

	assertNamedBucketContains(t, got, "components", "HeroBanner")
	assertFrameworksEqual(t, got, "react")
	assertNestedStringValue(t, got, "react", "boundary", "shared")
	assertNestedStringSliceEqual(t, got, "react", "component_exports", []string{"HeroBanner"})
	assertNestedStringSliceEqual(t, got, "react", "hooks_used", []string{})
}

func TestDefaultEngineParsePathTypeScriptDecoratorAndGenericParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "decorators.ts")
	writeTestFile(
		t,
		filePath,
		`@sealed
class Demo<T> {}

function identity<T>(value: T): T {
  return value;
}

type Box<T> = { value: T };
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

	assertNamedBucketContains(t, got, "classes", "Demo")
	assertNamedBucketContains(t, got, "functions", "identity")
	assertNamedBucketContains(t, got, "type_aliases", "Box")

	demoClass := findNamedBucketItem(t, got, "classes", "Demo")
	classDecorators, ok := demoClass["decorators"].([]string)
	if !ok {
		t.Fatalf("classes.Demo.decorators = %T, want []string", demoClass["decorators"])
	}
	if !reflect.DeepEqual(classDecorators, []string{"@sealed"}) {
		t.Fatalf("classes.Demo.decorators = %#v, want []string{\"@sealed\"}", classDecorators)
	}
	assertStringSliceFieldValue(t, demoClass, "type_parameters", []string{"T"})

	identityFn := findNamedBucketItem(t, got, "functions", "identity")
	functionDecorators, ok := identityFn["decorators"].([]string)
	if !ok {
		t.Fatalf("functions.identity.decorators = %T, want []string", identityFn["decorators"])
	}
	if len(functionDecorators) != 0 {
		t.Fatalf("functions.identity.decorators = %#v, want []", functionDecorators)
	}
	assertStringSliceFieldValue(t, identityFn, "type_parameters", []string{"T"})

	boxAlias := findNamedBucketItem(t, got, "type_aliases", "Box")
	assertStringSliceFieldValue(t, boxAlias, "type_parameters", []string{"T"})
}

func TestDefaultEngineParsePathTSXJSXComponentUsageParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "Widget.tsx")
	writeTestFile(
		t,
		filePath,
		`type WidgetProps = {
  label: string;
};

function ToolbarButton({ label }: WidgetProps) {
  return <button>{label}</button>;
}

export function WidgetPage() {
  return <ToolbarButton label="hello" />;
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

	assertNamedBucketContains(t, got, "type_aliases", "WidgetProps")
	assertNamedBucketContains(t, got, "function_calls", "ToolbarButton")
}

func TestDefaultEngineParsePathTSXClassComponentParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "LegacyWidget.tsx")
	writeTestFile(
		t,
		filePath,
		`import React from "react";

type LegacyWidgetProps = {
  title: string;
};

export class LegacyWidget extends React.Component<LegacyWidgetProps> {
  render() {
    return <section>{this.props.title}</section>;
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

	assertNamedBucketContains(t, got, "classes", "LegacyWidget")
	assertNamedBucketContains(t, got, "functions", "render")
	assertNamedBucketContains(t, got, "type_aliases", "LegacyWidgetProps")
}

func findNamedBucketItem(t *testing.T, payload map[string]any, key string, name string) map[string]any {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			return item
		}
	}
	t.Fatalf("%s missing item with name %q", key, name)
	return nil
}

func findAllNamedBucketItems(t *testing.T, payload map[string]any, key string, name string) []map[string]any {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	matches := make([]map[string]any, 0)
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			matches = append(matches, item)
		}
	}
	return matches
}

func assertFrameworksEqual(t *testing.T, payload map[string]any, want ...string) {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	got, ok := semantics["frameworks"].([]string)
	if !ok {
		t.Fatalf("framework_semantics.frameworks = %T, want []string", semantics["frameworks"])
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frameworks = %#v, want %#v", got, want)
	}
}

func assertNestedStringValue(t *testing.T, payload map[string]any, section string, key string, want string) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	got, ok := nested[key].(string)
	if !ok {
		t.Fatalf("framework_semantics.%s.%s = %T, want string", section, key, nested[key])
	}
	if got != want {
		t.Fatalf("framework_semantics.%s.%s = %#v, want %#v", section, key, got, want)
	}
}

func assertNestedStringSliceEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	key string,
	want []string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	got, ok := nested[key].([]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.%s = %T, want []string", section, key, nested[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("framework_semantics.%s.%s = %#v, want %#v", section, key, got, want)
	}
}

func assertNestedRouteEntriesEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	want []map[string]string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	raw, ok := nested["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.route_entries = %T, want []map[string]string", section, nested["route_entries"])
	}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("framework_semantics.%s.route_entries = %#v, want %#v", section, raw, want)
	}
}

func TestDetectExpressSemanticRequiresImport(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "hooks", "useGraphFilters.js")
	writeTestFile(
		t,
		filePath,
		`import React, { useState, useMemo, useEffect } from "react";

const urlSearchParams = new URLSearchParams(window.location.search);
const searchParams = Object.fromEntries(urlSearchParams.entries());
const cookies = document.cookie;

function getFilters() {
  urlSearchParams.get("soldDateRange");
  searchParams.get("year");
  cookies.get("soldDate");
  searchParams.delete("year");
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

	// Without an express import, no express framework should be detected.
	assertFrameworksEqual(t, got, "react")
	semantics := frameworkSemanticsMap(t, got)
	if _, ok := semantics["express"]; ok {
		t.Fatal("express section present without express import, want absent")
	}
}

func frameworkSemanticsMap(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics = %T, want map[string]any", payload["framework_semantics"])
	}
	return semantics
}

func nestedSemanticsSection(t *testing.T, payload map[string]any, section string) map[string]any {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	nested, ok := semantics[section].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics.%s = %T, want map[string]any", section, semantics[section])
	}
	return nested
}
