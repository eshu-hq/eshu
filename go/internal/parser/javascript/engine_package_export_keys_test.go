// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// These tests pin the #7601 cross-repository keys. A producer's exported
// top-level function or class carries package_id (its nearest package.json
// name) and export_name; a consumer's real call bound to a bare package import
// carries package_export_symbol = package:<source>#<imported name>. The reducer
// joins the two, so every case where a key would name the wrong symbol must
// emit no key at all.

func parsePackageKeyFixture(t *testing.T, repoRoot string, filePath string) map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}
	return got
}

// callPackageKey returns the package_export_symbol of the one call with the
// given full name and call kind, failing when the call is missing.
func callPackageKey(t *testing.T, payload map[string]any, fullName string, callKind string) string {
	t.Helper()

	calls, _ := payload["function_calls"].([]map[string]any)
	for _, call := range calls {
		if call["full_name"] == fullName && call["call_kind"] == callKind {
			key, _ := call["package_export_symbol"].(string)
			return key
		}
	}
	t.Fatalf("function_calls has no %q call with call_kind %q in %#v", fullName, callKind, calls)
	return ""
}

func definitionKeys(t *testing.T, payload map[string]any, bucket string, name string) (string, string) {
	t.Helper()

	item := assertBucketItemByName(t, payload, bucket, name)
	packageID, _ := item["package_id"].(string)
	exportName, _ := item["export_name"].(string)
	return packageID, exportName
}

func TestDefaultEngineParsePathTypeScriptStampsProducerPackageExportKeys(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": " @acme/format ", "main": "dist/index.js"}`)
	entryPath := filepath.Join(repoRoot, "src", "index.ts")
	writeTestFile(t, entryPath, `export function formatPrice(value: number) {
  return inner(value);
}

function inner(value: number) {
  return value;
}

export class PriceFormatter {
  format() {}
}

export abstract class BaseFormatter {}

export const toCents = (value: number) => value * 100;

export default function render() {}

export function outer() {
  function nested() {}
  return nested;
}

export namespace Money {
  export function inNamespace() {}
}
`)

	got := parsePackageKeyFixture(t, repoRoot, entryPath)

	for _, tc := range []struct {
		bucket     string
		name       string
		wantID     string
		wantExport string
	}{
		{"functions", "formatPrice", "@acme/format", "formatPrice"},
		{"classes", "PriceFormatter", "@acme/format", "PriceFormatter"},
		{"classes", "BaseFormatter", "@acme/format", "BaseFormatter"},
		{"functions", "toCents", "@acme/format", "toCents"},
		{"functions", "render", "@acme/format", "default"},
		{"functions", "outer", "@acme/format", "outer"},
		// Not exported, nested inside an export, a class member, or exported
		// from a TypeScript namespace: none is a package export.
		{"functions", "inner", "", ""},
		{"functions", "nested", "", ""},
		{"functions", "format", "", ""},
		{"functions", "inNamespace", "", ""},
	} {
		gotID, gotExport := definitionKeys(t, got, tc.bucket, tc.name)
		if gotID != tc.wantID || gotExport != tc.wantExport {
			t.Errorf("%s %q keys = (%q, %q), want (%q, %q)", tc.bucket, tc.name, gotID, gotExport, tc.wantID, tc.wantExport)
		}
	}
}

func TestDefaultEngineParsePathTypeScriptKeepsDefaultExportKeysToPackageEntryFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "@acme/format", "main": "dist/index.js"}`)
	internalPath := filepath.Join(repoRoot, "src", "lib", "helper.ts")
	writeTestFile(t, internalPath, `export default function helper() {}

export function namedHelper() {}
`)

	got := parsePackageKeyFixture(t, repoRoot, internalPath)

	// A default export of an internal module is not the package's default
	// export, so it carries no key; its named export still does.
	if gotID, gotExport := definitionKeys(t, got, "functions", "helper"); gotID != "" || gotExport != "" {
		t.Errorf("internal default export keys = (%q, %q), want none", gotID, gotExport)
	}
	if gotID, gotExport := definitionKeys(t, got, "functions", "namedHelper"); gotID != "@acme/format" || gotExport != "namedHelper" {
		t.Errorf("internal named export keys = (%q, %q), want (@acme/format, namedHelper)", gotID, gotExport)
	}
}

func TestDefaultEngineParsePathTypeScriptKeepsDefaultExportKeysOffSubpathExports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@acme/format",
  "main": "dist/index.js",
  "exports": {".": "./dist/index.js", "./plugin/*": "./dist/plugin/*.js"}
}`)
	pluginPath := filepath.Join(repoRoot, "src", "plugin", "audit.ts")
	writeTestFile(t, pluginPath, "export default function audit() {}\n")
	entryPath := filepath.Join(repoRoot, "src", "index.ts")
	writeTestFile(t, entryPath, "export default function render() {}\n")

	// The plugin file's default export is the default of "@acme/format/plugin/audit",
	// not of "@acme/format", so it must not claim package:@acme/format#default.
	plugin := parsePackageKeyFixture(t, repoRoot, pluginPath)
	if gotID, gotExport := definitionKeys(t, plugin, "functions", "audit"); gotID != "" || gotExport != "" {
		t.Errorf("subpath default export keys = (%q, %q), want none", gotID, gotExport)
	}
	entry := parsePackageKeyFixture(t, repoRoot, entryPath)
	if gotID, gotExport := definitionKeys(t, entry, "functions", "render"); gotID != "@acme/format" || gotExport != "default" {
		t.Errorf("main entry default export keys = (%q, %q), want (@acme/format, default)", gotID, gotExport)
	}
}

func TestDefaultEngineParsePathTypeScriptSkipsProducerKeysWithoutNamedManifest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		manifest string
	}{
		{name: "no manifest"},
		{name: "unnamed manifest", manifest: `{"private": true}`},
		{name: "non-string name", manifest: `{"name": 42, "main": "index.js"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			if tc.manifest != "" {
				writeTestFile(t, filepath.Join(repoRoot, "package.json"), tc.manifest)
			}
			filePath := filepath.Join(repoRoot, "index.ts")
			writeTestFile(t, filePath, "export function run() {}\n")

			got := parsePackageKeyFixture(t, repoRoot, filePath)
			if gotID, gotExport := definitionKeys(t, got, "functions", "run"); gotID != "" || gotExport != "" {
				t.Errorf("keys = (%q, %q), want none", gotID, gotExport)
			}
		})
	}
}

func TestDefaultEngineParsePathTSXStampsConsumerPackageExportSymbols(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app"}`)
	writeTestFile(t, filepath.Join(repoRoot, "tsconfig.json"), `{"compilerOptions": {"baseUrl": "src"}}`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "shared.ts"), "export function local() {}\n")
	filePath := filepath.Join(repoRoot, "src", "page.tsx")
	writeTestFile(t, filePath, `import { formatPrice, PriceFormatter as PF } from "@acme/format";
import render from "@acme/format";
import * as fmt from "@acme/format";
import type { Money } from "@acme/format";
import { type Currency } from "@acme/format";
import { sub } from "@acme/format/sub";
import { rel } from "./rel";
import { local } from "shared";
import { Button } from "@acme/ui";
import { shadowed } from "@acme/shadow";
import { dup } from "@acme/dup";
import { destructured } from "@acme/destructured";
import { caught } from "@acme/caught";
const util = require("@acme/util");
const { pick } = require("@acme/util");

export function Page(amount: Money, formatter: PF) {
  formatPrice(1);
  new PF();
  render();
  fmt.formatPrice(2);
  new fmt.PriceFormatter();
  fmt.nested.deep();
  obj.formatPrice();
  render.member();
  Money();
  Currency();
  sub();
  rel();
  local();
  util.pick();
  pick();
  shadowed();
  dup();
  destructured();
  caught();
  // formatPrice in a comment and "formatPrice" in a string bind nothing.
  const label = "formatPrice";
  return <Button />;
}

function withPattern({ destructured }: { destructured: number }) {
  try {
    return destructured;
  } catch (caught) {
    return caught;
  }
}

function withParameter(shadowed: number) {
  return shadowed;
}

function withLocal() {
  const dup = () => 1;
  return dup;
}
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)

	for _, tc := range []struct {
		fullName string
		callKind string
		want     string
	}{
		{"formatPrice", "function_call", "package:@acme/format#formatPrice"},
		{"PF", "constructor_call", "package:@acme/format#PriceFormatter"},
		{"render", "function_call", "package:@acme/format#default"},
		{"fmt.formatPrice", "function_call", "package:@acme/format#formatPrice"},
		{"fmt.PriceFormatter", "constructor_call", "package:@acme/format#PriceFormatter"},
		{"util.pick", "function_call", "package:@acme/util#pick"},
		{"pick", "function_call", "package:@acme/util#pick"},
		{"Button", "jsx_component", "package:@acme/ui#Button"},
		// A deep chain, a member of a non-import, a member of a default import,
		// a type-only binding, a subpath, a relative or in-repo import, and a
		// name the file also declares elsewhere all stay unkeyed.
		{"fmt.nested.deep", "function_call", ""},
		{"obj.formatPrice", "function_call", ""},
		{"render.member", "function_call", ""},
		{"Money", "function_call", ""},
		{"Currency", "function_call", ""},
		{"sub", "function_call", ""},
		{"rel", "function_call", ""},
		{"local", "function_call", ""},
		{"shadowed", "function_call", ""},
		{"dup", "function_call", ""},
		{"destructured", "function_call", ""},
		{"caught", "function_call", ""},
		// Type references stay emitted for type liveness and carry no key.
		{"Money", "typescript.type_reference", ""},
		{"PF", "typescript.type_reference", ""},
	} {
		if got := callPackageKey(t, got, tc.fullName, tc.callKind); got != tc.want {
			t.Errorf("%s (%s) package_export_symbol = %q, want %q", tc.fullName, tc.callKind, got, tc.want)
		}
	}
}

func TestDefaultEngineParsePathTypeScriptKeysTestFileCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "page.test.ts")
	writeTestFile(t, filePath, `import { formatPrice } from "@acme/format";

formatPrice(3);
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)
	if key := callPackageKey(t, got, "formatPrice", "function_call"); key != "package:@acme/format#formatPrice" {
		t.Fatalf("test-file call package_export_symbol = %q, want package:@acme/format#formatPrice", key)
	}
}
