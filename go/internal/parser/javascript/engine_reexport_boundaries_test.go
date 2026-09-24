// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// Regression tests for issue #7056: an export declaration (export class, export
// const, ...) that has no module source must never produce a reexport import,
// however the word "from" appears later in its body, comments or strings.

func parseJavaScriptFamilyFixture(t *testing.T, name string, body string) map[string]any {
	t.Helper()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", name)
	writeTestFile(t, filePath, body)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	return got
}

func assertNoReExportImports(t *testing.T, payload map[string]any) {
	t.Helper()

	items, _ := payload["imports"].([]map[string]any)
	for _, item := range items {
		if importType, _ := item["import_type"].(string); importType == "reexport" {
			t.Fatalf("imports contains reexport %#v, want none", item)
		}
	}
}

func TestDefaultEngineParsePathExportDeclarationIsNotAReExport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file string
		body string
	}{
		{
			name: "typescript class followed by doc comment",
			file: "orchestrator.ts",
			body: `export class Orchestrator {
  private readonly basePage = 1;
  run(): void { f(a, this.basePage); }
}

/**
 * The page id comes from ./upstream and is cached for the session.
 */
export const after = 1;
`,
		},
		{
			name: "typescript class with from inside a doc comment in the body",
			file: "inner.ts",
			body: `export class Orchestrator {
  m() { f(a, this.basePage); }
  /** Values come from ./config via the loader. */
  n() { return 1; }
}
`,
		},
		{
			name: "typescript class with from inside a string literal",
			file: "strings.ts",
			body: `export class Loader {
  m() { f(a, this.basePage); }
  n() { return "loaded from ./cache"; }
}
`,
		},
		{
			name: "typescript class with from inside a template literal",
			file: "template.ts",
			body: "export class Loader {\n  m() { f(a, this.basePage); }\n  n(x: string) { return `read ${x} from ./cache`; }\n}\n",
		},
		{
			name: "javascript class with from inside a doc comment in the body",
			file: "orchestrator.js",
			body: `export class Orchestrator {
  m() { f(a, this.basePage); }
  /**
   * The page id comes from ./upstream and is cached for the session.
   */
  n() { return 1; }
}
`,
		},
		{
			name: "javascript const with from inside a string literal",
			file: "value.js",
			body: `export const message = "copied from ./elsewhere";
`,
		},
		{
			name: "typescript default export with from in a line comment",
			file: "default.ts",
			body: `export default function run() {
  // taken from ./legacy
  return 1;
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNoReExportImports(t, parseJavaScriptFamilyFixture(t, tc.file, tc.body))
		})
	}
}

func TestDefaultEngineParsePathRealReExportFormsKeepNameAndSource(t *testing.T) {
	t.Parallel()

	got := parseJavaScriptFamilyFixture(t, "index.ts", `export { encode } from "./jwt";
export { decode as verify } from './jwt';
export * from "./ssm";
export * as helpers from "./helpers";
export type { Config } from "./config";
export {
  alpha,
  // a line comment between specifiers
  beta as gamma,
} from "./multi";
export /* before clause */ { delta } /* between */ from /* before source */ "./commented";
`)

	cases := []struct {
		name     string
		source   string
		original string
	}{
		{name: "encode", source: "./jwt", original: "encode"},
		{name: "verify", source: "./jwt", original: "decode"},
		{name: "Config", source: "./config", original: "Config"},
		{name: "alpha", source: "./multi", original: "alpha"},
		{name: "gamma", source: "./multi", original: "beta"},
		{name: "delta", source: "./commented", original: "delta"},
	}
	for _, tc := range cases {
		item := findNamedBucketItem(t, got, "imports", tc.name)
		assertStringFieldValue(t, item, "source", tc.source)
		assertStringFieldValue(t, item, "import_type", "reexport")
		assertStringFieldValue(t, item, "original_name", tc.original)
	}

	starSources := map[string]bool{}
	items, _ := got["imports"].([]map[string]any)
	for _, item := range items {
		if name, _ := item["name"].(string); name == "*" {
			source, _ := item["source"].(string)
			starSources[source] = true
		}
	}
	for _, want := range []string{"./ssm", "./helpers"} {
		if !starSources[want] {
			t.Fatalf("star reexport sources = %v, want %q present", starSources, want)
		}
	}
}

// TestDefaultEngineParsePathCommonJSModuleExportsRequire pins today's
// behaviour for `module.exports = require("./x")`: it is an assignment, not a
// require declarator or an ESM re-export, so the parser emits no import entry
// for it. The #7056 fix must not change that.
func TestDefaultEngineParsePathCommonJSModuleExportsRequire(t *testing.T) {
	t.Parallel()

	got := parseJavaScriptFamilyFixture(t, "cjs.js", `module.exports = require("./impl");
`)
	items, _ := got["imports"].([]map[string]any)
	for _, item := range items {
		if source, _ := item["source"].(string); source == "./impl" {
			t.Fatalf("imports contains %#v, want no entry for module.exports = require", item)
		}
	}
}

// TestDefaultEngineParsePathDropsOversizedImportSource proves the defensive
// bound: a module specifier longer than any legitimate one never reaches the
// imports bucket, because it would become an unindexable Module node name.
func TestDefaultEngineParsePathDropsOversizedImportSource(t *testing.T) {
	t.Parallel()

	oversized := "./" + strings.Repeat("a", 2048)
	got := parseJavaScriptFamilyFixture(t, "big.ts", `import { ok } from "./fine";
import { huge } from "`+oversized+`";
export { alsoHuge } from "`+oversized+`";
`)

	findNamedBucketItem(t, got, "imports", "ok")
	items, _ := got["imports"].([]map[string]any)
	for _, item := range items {
		if source, _ := item["source"].(string); len(source) > 1024 {
			t.Fatalf("imports contains source of %d bytes (%s), want it dropped", len(source), item["name"])
		}
	}
}
