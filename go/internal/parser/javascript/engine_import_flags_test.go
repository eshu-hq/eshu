// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// typeScriptImportFlagFixture covers every TypeScript spelling of a type-only
// import or re-export next to the value forms that must stay unflagged (issue
// #7344). A type-only import is erased at compile time, so it cannot close a
// runtime import cycle.
const typeScriptImportFlagFixture = `import type { A } from "./a";
import type D from "./d";
import type * as NS from "./ns";
import { type B, C } from "./bc";
import { type E as EE } from "./e";
import { F } from "./f";
import G from "./g";
import type J = require("./j");
import K = require("./k");
import "./side-effect";
export type { H } from "./h";
export type * from "./star";
export { I } from "./i";
export * from "./allvalues";
export { type Z, W } from "./zw";
`

// TestDefaultEngineParsePathTypeScriptImportFlags is the #7344 TS/TSX fixture
// gate: type_only is set for `import type`, for a per-specifier `type`
// modifier, and for `export type` re-exports, and stays absent for every value
// form so unflagged rows are byte-identical to the pre-#7344 payload.
func TestDefaultEngineParsePathTypeScriptImportFlags(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{"ts", "tsx"} {
		ext := ext
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			filePath := filepath.Join(repoRoot, "src", "index."+ext)
			writeTestFile(t, filePath, typeScriptImportFlagFixture)

			engine, err := parser.DefaultEngine()
			if err != nil {
				t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
			}
			got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
			if err != nil {
				t.Fatalf("ParsePath() error = %v, want nil", err)
			}

			tests := []struct {
				name     string
				source   string
				typeOnly bool
			}{
				{name: "A", source: "./a", typeOnly: true},
				{name: "default", source: "./d", typeOnly: true},
				{name: "*", source: "./ns", typeOnly: true},
				{name: "B", source: "./bc", typeOnly: true},
				{name: "C", source: "./bc"},
				{name: "E", source: "./e", typeOnly: true},
				{name: "F", source: "./f"},
				{name: "default", source: "./g"},
				{name: "*", source: "./j", typeOnly: true},
				{name: "*", source: "./k"},
				{name: "./side-effect", source: "./side-effect"},
				{name: "H", source: "./h", typeOnly: true},
				{name: "*", source: "./star", typeOnly: true},
				{name: "I", source: "./i"},
				{name: "*", source: "./allvalues"},
				{name: "Z", source: "./zw", typeOnly: true},
				{name: "W", source: "./zw"},
			}
			for _, tt := range tests {
				item := findImportEntry(t, got, tt.name, tt.source)
				assertTypeOnlyFlag(t, item, tt.typeOnly)
				assertNoOtherImportFlags(t, item)
			}
		})
	}
}

// TestDefaultEngineParsePathJavaScriptImportFlagsAbsent proves plain JavaScript
// imports, re-exports, and require forms never carry a flag: JavaScript has no
// type-only syntax, and #7344 emits deferred only where the parser already
// walks function bodies.
func TestDefaultEngineParsePathJavaScriptImportFlagsAbsent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "index.js")
	writeTestFile(t, filePath, `import { a } from "./a";
import b from "./b";
export { c } from "./c";
const d = require("./d");
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["imports"].([]map[string]any)
	if !ok || len(items) == 0 {
		t.Fatalf("imports = %#v, want non-empty []map[string]any", got["imports"])
	}
	for _, item := range items {
		assertTypeOnlyFlag(t, item, false)
		assertNoOtherImportFlags(t, item)
	}
}

// findImportEntry returns the imports row with the given name and source.
func findImportEntry(t *testing.T, payload map[string]any, name string, source string) map[string]any {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		if got, _ := item["name"].(string); got != name {
			continue
		}
		if got, _ := item["source"].(string); got != source {
			continue
		}
		return item
	}
	t.Fatalf("imports missing name=%q source=%q in %#v", name, source, items)
	return nil
}

// assertTypeOnlyFlag requires type_only to be boolean true when want is true
// and absent when want is false; a stored false would change every unflagged
// row.
func assertTypeOnlyFlag(t *testing.T, item map[string]any, want bool) {
	t.Helper()

	value, present := item["type_only"]
	if !want {
		if present {
			t.Fatalf("import %v/%v type_only = %#v, want absent", item["name"], item["source"], value)
		}
		return
	}
	if got, _ := value.(bool); !got {
		t.Fatalf("import %v/%v type_only = %#v, want true", item["name"], item["source"], value)
	}
}

// assertNoOtherImportFlags requires the flags a TS/JS import never earns to be
// absent, so a flag cannot leak onto a language that has no source for it.
func assertNoOtherImportFlags(t *testing.T, item map[string]any) {
	t.Helper()

	for _, flag := range []string{"deferred", "inferred"} {
		if value, present := item[flag]; present {
			t.Fatalf("import %v/%v flag %q = %#v, want absent", item["name"], item["source"], flag, value)
		}
	}
}
