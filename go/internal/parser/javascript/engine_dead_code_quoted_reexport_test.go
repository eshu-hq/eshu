// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// A package barrel may spell the re-exported symbol as a string literal
// (`export { 'Widget' as Public } from "./widget"`). The public-surface walk
// must resolve it to the same symbol as the identifier spelling, so the
// original declaration stays a root (issue #7461).
func TestDefaultEngineParsePathTypeScriptMarksQuotedNameBarrelReExportSurface(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{
  "name": "@example/quoted",
  "exports": {
    ".": "./src/index.ts"
  }
}
`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.ts"), `export { 'Widget' as PublicWidget } from "./widget";
export { "makeGadget" as 'make gadget' } from "./widget";
`)
	widgetPath := filepath.Join(repoRoot, "src", "widget.ts")
	writeTestFile(t, widgetPath, `export class Widget {}

export function makeGadget() {
  return 1;
}

function privateHelper() {
  return 2;
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, widgetPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "classes", "Widget"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "makeGadget"),
		"dead_code_root_kinds",
		"typescript.public_api_reexport",
	)
	if _, ok := assertFunctionByName(t, got, "privateHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("privateHelper dead_code_root_kinds present, want absent for private helper")
	}
}
