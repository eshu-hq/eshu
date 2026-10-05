// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"
)

// A bare import names a package only when a package.json on the path from the
// file up to the repository root declares it in a dependency field. Anything
// else that looks bare (a bundler or jsconfig alias, a Node.js built-in, an
// undeclared global) may resolve inside the repository, so it gets no key: a
// key there could link the call to an unrelated corpus package of that name.

func TestDefaultEngineParsePathJavaScriptSkipsUndeclaredBareImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "web-app"}`)
	writeTestFile(t, filepath.Join(repoRoot, "jsconfig.json"), `{"compilerOptions": {"baseUrl": "src"}}`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "api", "index.js"), "export function getUser() {}\n")
	filePath := filepath.Join(repoRoot, "src", "page.jsx")
	writeTestFile(t, filePath, `import { getUser } from "api";
import { readFile } from "fs";
import { format } from "util";
import { pick } from "lodash";

getUser();
readFile();
format();
pick();
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)
	for _, fullName := range []string{"getUser", "readFile", "format", "pick"} {
		if key := callPackageKey(t, got, fullName, "function_call"); key != "" {
			t.Errorf("%s package_export_symbol = %q, want none: no manifest declares the import", fullName, key)
		}
	}
}

func TestDefaultEngineParsePathTypeScriptKeysOnlyDeclaredDependencies(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "acme-root",
  "dependencies": {"@acme/hoisted": "1.0.0", "utils": "1.0.0"},
  "devDependencies": {"@acme/dev": "1.0.0"},
  "peerDependencies": {"@acme/peer": "1.0.0"},
  "optionalDependencies": {"@acme/optional": "1.0.0"}
}`)
	writeTestFile(t, filepath.Join(repoRoot, "packages", "app", "package.json"),
		`{"name": "@acme/app", "dependencies": {"@acme/nested": "1.0.0"}}`)
	writeTestFile(t, filepath.Join(repoRoot, "packages", "app", "tsconfig.json"),
		`{"compilerOptions": {"baseUrl": ".", "paths": {"utils": ["src/utils.ts"]}}}`)
	writeTestFile(t, filepath.Join(repoRoot, "packages", "app", "src", "utils.ts"), "export function local() {}\n")
	filePath := filepath.Join(repoRoot, "packages", "app", "src", "page.ts")
	writeTestFile(t, filePath, `import { hoisted } from "@acme/hoisted";
import { nested } from "@acme/nested";
import { peer } from "@acme/peer";
import { optional } from "@acme/optional";
import { local } from "utils";
import { aliasAt } from "@/x";
import { aliasTilde } from "~/x";
import { readFile } from "node:fs";
import { pick } from "lodash";

hoisted();
nested();
peer();
optional();
local();
aliasAt();
aliasTilde();
readFile();
pick();
`)
	testPath := filepath.Join(repoRoot, "packages", "app", "src", "page.test.ts")
	writeTestFile(t, testPath, `import { fixture } from "@acme/dev";

fixture();
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)
	for _, tc := range []struct {
		fullName string
		want     string
	}{
		{"hoisted", "package:@acme/hoisted#hoisted"},    // root manifest only
		{"nested", "package:@acme/nested#nested"},       // nearest manifest only
		{"peer", "package:@acme/peer#peer"},             // peerDependencies
		{"optional", "package:@acme/optional#optional"}, // optionalDependencies
		{"local", ""},      // tsconfig paths alias wins even though "utils" is declared
		{"aliasAt", ""},    // @/x is not a package specifier
		{"aliasTilde", ""}, // ~/x is not a package specifier
		{"readFile", ""},   // node: built-in
		{"pick", ""},       // undeclared
	} {
		if key := callPackageKey(t, got, tc.fullName, "function_call"); key != tc.want {
			t.Errorf("%s package_export_symbol = %q, want %q", tc.fullName, key, tc.want)
		}
	}

	testFile := parsePackageKeyFixture(t, repoRoot, testPath)
	if key := callPackageKey(t, testFile, "fixture", "function_call"); key != "package:@acme/dev#fixture" {
		t.Errorf("devDependency call from a test file package_export_symbol = %q, want package:@acme/dev#fixture", key)
	}
}

func TestDefaultEngineParsePathTypeScriptToleratesMalformedDependencyFields(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "acme-app",
  "dependencies": ["@acme/listed"],
  "peerDependencies": "@acme/text",
  "devDependencies": {"@acme/good": "1.0.0", "@acme/odd": 7}
}`)
	filePath := filepath.Join(repoRoot, "src", "page.ts")
	writeTestFile(t, filePath, `import { listed } from "@acme/listed";
import { text } from "@acme/text";
import { good } from "@acme/good";
import { odd } from "@acme/odd";

listed();
text();
good();
odd();
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)
	for _, tc := range []struct {
		fullName string
		want     string
	}{
		{"listed", ""}, // an array is not a dependency map
		{"text", ""},   // a string is not a dependency map
		{"good", "package:@acme/good#good"},
		{"odd", "package:@acme/odd#odd"}, // the key is declared; its version value is not read
	} {
		if key := callPackageKey(t, got, tc.fullName, "function_call"); key != tc.want {
			t.Errorf("%s package_export_symbol = %q, want %q", tc.fullName, key, tc.want)
		}
	}
}
