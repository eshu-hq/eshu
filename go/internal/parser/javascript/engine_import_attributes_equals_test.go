// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestDefaultEngineParsePathImportAttributesAndEquals is the issue #7059
// fixture gate: import rows must exist for import attributes (`with` and the
// older `assert` form) and for TypeScript import-equals forms, so the
// dependency, dead-code, and module graphs keep those edges.
func TestDefaultEngineParsePathImportAttributesAndEquals(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "index.ts")
	writeTestFile(
		t,
		filePath,
		`import data from "./data.json" with { type: "json" };
import cfg from "./cfg.json" assert { type: "json" };
import * as ns from "./ns.json" with { type: "json" };
import { a as b } from "./mod.json" assert { type: "json" };
export * from "./x.json" with { type: "json" };
export * from "./y.json" assert { type: "json" };
export { a } from "./z.json" with { type: "json" };
export * as models from "./models.json" with { type: "json" };
import x = require("./x");
export import X = require("./x");
import remote from "@vendor/remote" with { type: "json" };
export * from "@vendor/remote" with { type: "json" };
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

	// Plain imports with attributes already emit rows; these pin that behavior.
	assertImportRow(t, got, "default", "./data.json", "data")
	assertImportRow(t, got, "default", "./cfg.json", "cfg")
	assertImportRow(t, got, "*", "./ns.json", "ns")
	assertImportRow(t, got, "a", "./mod.json", "b")

	// Re-exports with attributes must emit reexport rows for relative sources.
	assertReexportRow(t, got, "*", "./x.json")
	assertReexportRow(t, got, "*", "./y.json")
	assertReexportRow(t, got, "a", "./z.json")
	assertReexportRow(t, got, "*", "./models.json")

	// Import-equals forms must emit require rows bound to the local name.
	assertRequireRow(t, got, "./x", "x")
	assertRequireRow(t, got, "./x", "X")

	// Non-relative attribute re-exports stay filtered, matching the existing
	// relative-only re-export contract; the static non-relative import still
	// emits its row.
	assertImportRow(t, got, "default", "@vendor/remote", "remote")
	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	for _, item := range items {
		if source, _ := item["source"].(string); source == "@vendor/remote" {
			if typ, _ := item["import_type"].(string); typ == "reexport" {
				t.Fatalf("imports contains reexport row for non-relative source in %#v", items)
			}
		}
	}
}

func assertImportRow(t *testing.T, payload map[string]any, name, source, alias string) {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		gotName, _ := item["name"].(string)
		gotSource, _ := item["source"].(string)
		gotAlias, _ := item["alias"].(string)
		if gotName == name && gotSource == source && gotAlias == alias {
			return
		}
	}
	t.Fatalf("imports missing name=%q source=%q alias=%q in %#v", name, source, alias, items)
}

func assertReexportRow(t *testing.T, payload map[string]any, name, source string) {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		gotName, _ := item["name"].(string)
		gotSource, _ := item["source"].(string)
		gotType, _ := item["import_type"].(string)
		if gotName == name && gotSource == source && gotType == "reexport" {
			return
		}
	}
	t.Fatalf("imports missing reexport name=%q source=%q in %#v", name, source, items)
}

func assertRequireRow(t *testing.T, payload map[string]any, source, alias string) {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		gotSource, _ := item["source"].(string)
		gotAlias, _ := item["alias"].(string)
		gotType, _ := item["import_type"].(string)
		if gotSource == source && gotAlias == alias && gotType == "require" {
			return
		}
	}
	t.Fatalf("imports missing require source=%q alias=%q in %#v", source, alias, items)
}
